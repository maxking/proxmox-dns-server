// Package proxmox provides management and discovery of Proxmox VE containers and VMs.
//
// This package handles communication with Proxmox VE to discover running instances,
// retrieve their IP addresses, and provide a thread-safe cache for DNS resolution.
package proxmox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"proxmox-dns-server/pkg/config"
)

// Standard error variables for common error conditions
var (
	ErrContainerNotFound = errors.New("container not found")
	ErrVMNotFound        = errors.New("VM not found")
	ErrInvalidID         = errors.New("invalid ID")
	ErrCommandTimeout    = errors.New("command execution timeout")
	ErrNoIPFound         = errors.New("no suitable IP address found")
	ErrInvalidIPPrefix   = errors.New("IP does not match configured prefix")
)

// ProxmoxInstance represents a Proxmox container or VM with its network information.
// It contains the essential data needed for DNS resolution.
type ProxmoxInstance struct {
	ID     int    `json:"vmid"`   // Proxmox instance ID (e.g., 102)
	Name   string `json:"name"`   // Instance name (e.g., "webserver")
	Status string `json:"status"` // Current status (e.g., "running", "stopped")
	Type   string `json:"type"`   // Instance type ("container" or "vm")
	IPv4   string `json:"ipv4"`   // Primary IPv4 address
}

// Manager manages the discovery and caching of Proxmox container and VM instances.
// It provides thread-safe access to instance data and handles periodic refreshes.
type Manager struct {
	instances sync.Map     // Thread-safe map storing instances by ID and name
	config    config.ProxmoxConfig // Configuration for Proxmox operations
}

// Proxmox ID validation constants
const (
	MinProxmoxID = 100       // Proxmox typical minimum ID
	MaxProxmoxID = 999999999 // Proxmox theoretical maximum ID (9 digits)
)

// NewManager creates a new Manager with the given configuration.
// It initializes the thread-safe instance storage.
func NewManager(cfg config.ProxmoxConfig) *Manager {
	return &Manager{
		config: cfg,
	}
}

// RefreshInstances updates the cached instance data by querying Proxmox VE.
// It discovers all containers and VMs, retrieves their IP addresses,
// and updates the internal cache. This method is thread-safe.
func (pm *Manager) RefreshInstances() error {
	// Clear all existing entries efficiently by creating a new sync.Map
	pm.instances = sync.Map{}

	if err := pm.loadContainers(); err != nil {
		return fmt.Errorf("proxmox refresh: failed to load containers: %w", err)
	}

	if err := pm.loadVMs(); err != nil {
		return fmt.Errorf("proxmox refresh: failed to load VMs: %w", err)
	}

	return nil
}

// loadContainers discovers and loads all Proxmox containers.
// It executes 'pct list' to get container information and retrieves IP addresses
// for running containers that match the configured IP prefix.
func (pm *Manager) loadContainers() error {
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pct", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("proxmox containers: failed to execute 'pct list' command: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			firstLine = false
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		status := fields[1]
		name := fields[2]

		// Only try to get IP for running containers
		if status != "running" {
			if pm.config.DebugMode {
				log.Printf("Debug: Container %d (%s) is %s, skipping IP detection", id, name, status)
			}
			continue
		}

		ipv4, err := pm.getContainerIP(id)
		if err != nil {
			log.Printf("Warning: Failed to get IP for container %d (%s): %v", id, name, err)
			continue
		}

		instance := ProxmoxInstance{
			ID:     id,
			Name:   name,
			Status: status,
			Type:   "container",
			IPv4:   ipv4,
		}

		pm.instances.Store(strconv.Itoa(id), instance)
		pm.instances.Store(name, instance)
	}

	return nil
}

// loadVMs discovers and loads all Proxmox virtual machines.
// It executes 'qm list' to get VM information and retrieves IP addresses
// for running VMs that match the configured IP prefix.
func (pm *Manager) loadVMs() error {
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qm", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("proxmox VMs: failed to execute 'qm list' command: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			firstLine = false
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		name := fields[1]
		status := fields[2]

		// Only try to get IP for running VMs
		if status != "running" {
			if pm.config.DebugMode {
				log.Printf("Debug: VM %d (%s) is %s, skipping IP detection", id, name, status)
			}
			continue
		}

		ipv4, err := pm.getVMIP(id)
		if err != nil {
			log.Printf("Warning: Failed to get IP for VM %d (%s): %v", id, name, err)
			continue
		}

		instance := ProxmoxInstance{
			ID:     id,
			Name:   name,
			Status: status,
			Type:   "vm",
			IPv4:   ipv4,
		}

		pm.instances.Store(strconv.Itoa(id), instance)
		pm.instances.Store(name, instance)
	}

	return nil
}

// getContainerIP retrieves the IP address of a Proxmox container by ID.
// It tries multiple methods: hostname -I, ip route, and container configuration.
// Returns the first IP address that matches the configured prefix.
func (pm *Manager) getContainerIP(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("container IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}
	idStr := strconv.Itoa(id)

	// First try hostname -I
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pct", "exec", idStr, "--", "hostname", "-I")
	output, err := cmd.Output()
	if err != nil {
		if pm.config.DebugMode {
			log.Printf("Debug: Container %d - hostname -I failed: %v", id, err)
		}

		// Try alternative command: ip route get 1.1.1.1 | head -1 | awk '{print $7}'
		ctx2, cancel2 := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
		defer cancel2()
		cmd = exec.CommandContext(ctx2, "pct", "exec", idStr, "--", "sh", "-c", "ip route get 1.1.1.1 2>/dev/null | head -1 | awk '{print $7}'")
		output, err = cmd.Output()
		if err != nil {
			if pm.config.DebugMode {
				log.Printf("Debug: Container %d - ip route get failed: %v", id, err)
			}

			// Try getting IP from container config
			return pm.getContainerIPFromConfig(id)
		}
	}

	if pm.config.DebugMode {
		log.Printf("Debug: Container %d - command output: %s", id, string(output))
	}
	return pm.filterIPv4(string(output))
}

// getContainerIPFromConfig retrieves the IP address from a container's configuration.
// This is used as a fallback when direct IP detection methods fail.
// It parses the container config for network interface definitions.
func (pm *Manager) getContainerIPFromConfig(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("container config lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pct", "config", strconv.Itoa(id))
	output, err := cmd.Output()
	if err != nil {
		if pm.config.DebugMode {
			log.Printf("Debug: Container %d - pct config failed: %v", id, err)
		}
		return "", fmt.Errorf("container %d config: failed to execute 'pct config' command: %w", id, err)
	}

	if pm.config.DebugMode {
		log.Printf("Debug: Container %d - config output: %s", id, string(output))
	}

	// Look for net0, net1, etc. lines with IP addresses
	// Use bytes.Split to avoid string allocation
	outputBytes := output
	for len(outputBytes) > 0 {
		// Find next newline
		lineEnd := bytes.IndexByte(outputBytes, '\n')
		var line []byte
		if lineEnd == -1 {
			line = outputBytes
			outputBytes = nil
		} else {
			line = outputBytes[:lineEnd]
			outputBytes = outputBytes[lineEnd+1:]
		}

		// Check if line starts with "net" and contains "ip="
		if bytes.HasPrefix(line, []byte("net")) && bytes.Contains(line, []byte("ip=")) {
			// Extract IP more efficiently without multiple splits
			if ipStart := bytes.Index(line, []byte("ip=")); ipStart != -1 {
				ipStart += 3 // Skip "ip="
				ipEnd := bytes.IndexAny(line[ipStart:], ",/")
				if ipEnd == -1 {
					ipEnd = len(line) - ipStart
				}
				ip := string(line[ipStart : ipStart+ipEnd])
				if strings.HasPrefix(ip, pm.config.IPPrefix) {
					if pm.config.DebugMode {
						log.Printf("Debug: Container %d - Found IP in config: %s", id, ip)
					}
					return ip, nil
				}
			}
		}
	}

	return "", fmt.Errorf("container %d config: %w with prefix %s", id, ErrNoIPFound, pm.config.IPPrefix)
}

// getVMIP retrieves the IP address of a Proxmox virtual machine by ID.
// It uses the QEMU guest agent to query network interface information
// and returns the first IPv4 address matching the configured prefix.
func (pm *Manager) getVMIP(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("VM IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qm", "guest", "cmd", strconv.Itoa(id), "network-get-interfaces")
	output, err := cmd.Output()
	if err != nil {
		if pm.config.DebugMode {
			log.Printf("Debug: VM %d - qm guest cmd failed: %v", id, err)
		}
		return "", fmt.Errorf("VM %d network interfaces: failed to execute 'qm guest cmd network-get-interfaces': %w", id, err)
	}

	if pm.config.DebugMode {
		log.Printf("Debug: VM %d - qm guest cmd output: %s", id, string(output))
	}

	// Use specific types for better performance instead of interface{}
	type NetworkInterface struct {
		Name        string `json:"name"`
		IPAddresses []struct {
			IPAddress     string `json:"ip-address"`
			IPAddressType string `json:"ip-address-type"`
		} `json:"ip-addresses"`
	}

	var interfaces []NetworkInterface
	if err := json.Unmarshal(output, &interfaces); err != nil {
		if pm.config.DebugMode {
			log.Printf("Debug: VM %d - JSON unmarshal failed: %v", id, err)
			log.Printf("Debug: VM %d - Raw JSON output: %s", id, string(output))
		}
		return "", fmt.Errorf("VM %d network interfaces: failed to parse JSON response: %w", id, err)
	}

	if pm.config.DebugMode {
		log.Printf("Debug: VM %d - Found %d network interfaces", id, len(interfaces))
	}

	// Process interfaces using typed structures for better performance
	for i, iface := range interfaces {
		if pm.config.DebugMode {
			log.Printf("Debug: VM %d - Interface %d (%s)", id, i, iface.Name)
		}

		if pm.config.DebugMode {
			log.Printf("Debug: VM %d - Interface %s has %d IP addresses", id, iface.Name, len(iface.IPAddresses))
		}

		for j, ipAddr := range iface.IPAddresses {
			if pm.config.DebugMode {
				log.Printf("Debug: VM %d - IP %d: type=%s addr=%s", id, j, ipAddr.IPAddressType, ipAddr.IPAddress)
			}

			if ipAddr.IPAddressType == "ipv4" {
				if pm.config.DebugMode {
					log.Printf("Debug: VM %d - Found IPv4: %s", id, ipAddr.IPAddress)
				}
				if strings.HasPrefix(ipAddr.IPAddress, pm.config.IPPrefix) {
					if pm.config.DebugMode {
						log.Printf("Debug: VM %d - Using IPv4: %s", id, ipAddr.IPAddress)
					}
					return ipAddr.IPAddress, nil
				}
			}
		}
	}

	if pm.config.DebugMode {
		log.Printf("Debug: VM %d - No suitable IPv4 address found with prefix %s", id, pm.config.IPPrefix)
	}
	return "", fmt.Errorf("VM %d: %w with prefix %s", id, ErrNoIPFound, pm.config.IPPrefix)
}

// filterIPv4 parses command output to find valid IPv4 addresses.
// It returns the first IP address that matches the configured prefix.
func (pm *Manager) filterIPv4(output string) (string, error) {
	// Trim whitespace once
	trimmed := strings.TrimSpace(output)

	// Parse IPs without allocating intermediate slice
	start := 0
	for i := 0; i <= len(trimmed); i++ {
		// Check for word boundary (space or end of string)
		if i == len(trimmed) || trimmed[i] == ' ' || trimmed[i] == '\t' || trimmed[i] == '\n' {
			if i > start {
				ip := trimmed[start:i]
				// Skip multiple spaces
				if ip != "" && net.ParseIP(ip) != nil && strings.HasPrefix(ip, pm.config.IPPrefix) {
					return ip, nil
				}
			}
			// Skip whitespace
			for i < len(trimmed) && (trimmed[i] == ' ' || trimmed[i] == '\t' || trimmed[i] == '\n') {
				i++
			}
			start = i
		}
	}
	return "", fmt.Errorf("IP filtering: %w with prefix %s in output: %s", ErrNoIPFound, pm.config.IPPrefix, trimmed)
}

// GetInstanceByIdentifier retrieves a Proxmox instance by its identifier.
// The identifier can be either the instance ID (as string) or the instance name.
// Returns the instance and a boolean indicating if it was found.
func (pm *Manager) GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool) {
	value, exists := pm.instances.Load(identifier)
	if !exists {
		return ProxmoxInstance{}, false
	}
	instance, ok := value.(ProxmoxInstance)
	return instance, ok
}