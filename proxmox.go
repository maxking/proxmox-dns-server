// Package main implements a DNS server for Proxmox VE environments
// that resolves container and VM names to their IP addresses.
//
// The server automatically discovers Proxmox containers and VMs,
// caches their information, and provides DNS A record responses
// for queries matching the configured zone.
package main

import (
	"bufio"
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
	"time"
)

// Standard error variables for common error conditions
var (
	ErrContainerNotFound   = errors.New("container not found")
	ErrVMNotFound          = errors.New("VM not found")
	ErrInvalidID           = errors.New("invalid ID")
	ErrCommandTimeout      = errors.New("command execution timeout")
	ErrNoIPFound           = errors.New("no suitable IP address found")
	ErrInvalidIPPrefix     = errors.New("IP does not match configured prefix")
	ErrInvalidProxmoxConfig = errors.New("invalid proxmox configuration")
)

// ProxmoxConfig holds configuration parameters for Proxmox operations.
// It defines how the system interacts with Proxmox VE containers and VMs.
type ProxmoxConfig struct {
	IPPrefix       string        // IP prefix filter for container/VM IPs (e.g., "192.168.")
	CommandTimeout time.Duration // Timeout for executing Proxmox commands
	DebugMode      bool          // Enable debug logging for Proxmox operations
}

// Validate checks if the ProxmoxConfig contains valid configuration values.
// It returns an error if any required field is empty or invalid.
func (pc *ProxmoxConfig) Validate() error {
	if pc.IPPrefix == "" {
		return fmt.Errorf("%w: IP prefix is required", ErrInvalidProxmoxConfig)
	}
	
	if pc.CommandTimeout <= 0 {
		return fmt.Errorf("%w: command timeout must be positive", ErrInvalidProxmoxConfig)
	}
	
	return nil
}

// NewProxmoxConfigFromFlags creates a ProxmoxConfig with the specified IP prefix
// and default values for timeout and debug mode.
func NewProxmoxConfigFromFlags(ipPrefix string) *ProxmoxConfig {
	return &ProxmoxConfig{
		IPPrefix:       ipPrefix,
		CommandTimeout: 30 * time.Second, // Default command timeout
		DebugMode:      false,             // Default debug mode
	}
}

// ProxmoxInstance represents a Proxmox container or VM with its network information.
// It contains the essential data needed for DNS resolution.
type ProxmoxInstance struct {
	ID     int    `json:"vmid"`   // Proxmox instance ID (e.g., 102)
	Name   string `json:"name"`   // Instance name (e.g., "webserver")
	Status string `json:"status"` // Current status (e.g., "running", "stopped")
	Type   string `json:"type"`   // Instance type ("container" or "vm")
	IPv4   string `json:"ipv4"`   // Primary IPv4 address
}

// ProxmoxManager manages the discovery and caching of Proxmox container and VM instances.
// It provides thread-safe access to instance data and handles periodic refreshes.
type ProxmoxManager struct {
	instances sync.Map      // Thread-safe map storing instances by ID and name
	config    ProxmoxConfig // Configuration for Proxmox operations
}

// Proxmox ID validation constants
const (
	MinProxmoxID = 100       // Proxmox typical minimum ID
	MaxProxmoxID = 999999999 // Proxmox theoretical maximum ID (9 digits)
)

// NewProxmoxManager creates a new ProxmoxManager with the given configuration.
// It initializes the thread-safe instance storage.
func NewProxmoxManager(config ProxmoxConfig) *ProxmoxManager {
	return &ProxmoxManager{
		config: config,
	}
}

// RefreshInstances updates the cached instance data by querying Proxmox VE.
// It discovers all containers and VMs, retrieves their IP addresses,
// and updates the internal cache. This method is thread-safe.
func (pm *ProxmoxManager) RefreshInstances() error {
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
func (pm *ProxmoxManager) loadContainers() error {
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pct", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("proxmox containers: failed to execute 'pct list' command: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
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
func (pm *ProxmoxManager) loadVMs() error {
	ctx, cancel := context.WithTimeout(context.Background(), pm.config.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qm", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("proxmox VMs: failed to execute 'qm list' command: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
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
func (pm *ProxmoxManager) getContainerIP(id int) (string, error) {
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
func (pm *ProxmoxManager) getContainerIPFromConfig(id int) (string, error) {
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
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "net") && strings.Contains(line, "ip=") {
			// Extract IP from line like: net0: name=eth0,bridge=vmbr0,ip=192.168.1.100/24,gw=192.168.1.1
			parts := strings.Split(line, ",")
			for _, part := range parts {
				if strings.HasPrefix(part, "ip=") {
					ipWithMask := strings.TrimPrefix(part, "ip=")
					ip := strings.Split(ipWithMask, "/")[0]
					if strings.HasPrefix(ip, pm.config.IPPrefix) {
						if pm.config.DebugMode {
						log.Printf("Debug: Container %d - Found IP in config: %s", id, ip)
					}
						return ip, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("container %d config: %w with prefix %s", id, ErrNoIPFound, pm.config.IPPrefix)
}

// getVMIP retrieves the IP address of a Proxmox virtual machine by ID.
// It uses the QEMU guest agent to query network interface information
// and returns the first IPv4 address matching the configured prefix.
func (pm *ProxmoxManager) getVMIP(id int) (string, error) {
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

	var interfaces []interface{}
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
	for i, iface := range interfaces {
		if ifaceMap, ok := iface.(map[string]interface{}); ok {
			ifaceName := "unknown"
			if name, ok := ifaceMap["name"].(string); ok {
				ifaceName = name
			}
			if pm.config.DebugMode {
				log.Printf("Debug: VM %d - Interface %d (%s)", id, i, ifaceName)
			}

			if ipAddresses, ok := ifaceMap["ip-addresses"].([]interface{}); ok {
				if pm.config.DebugMode {
					log.Printf("Debug: VM %d - Interface %s has %d IP addresses", id, ifaceName, len(ipAddresses))
				}
				for j, ip := range ipAddresses {
					if ipMap, ok := ip.(map[string]interface{}); ok {
						if pm.config.DebugMode {
							log.Printf("Debug: VM %d - IP %d: %+v", id, j, ipMap)
						}
						if ipType, ok := ipMap["ip-address-type"].(string); ok && ipType == "ipv4" {
							if ipAddr, ok := ipMap["ip-address"].(string); ok {
								if pm.config.DebugMode {
									log.Printf("Debug: VM %d - Found IPv4: %s", id, ipAddr)
								}
								if strings.HasPrefix(ipAddr, pm.config.IPPrefix) {
									if pm.config.DebugMode {
										log.Printf("Debug: VM %d - Using IPv4: %s", id, ipAddr)
									}
									return ipAddr, nil
								}
							}
						}
					}
				}
			} else {
				if pm.config.DebugMode {
					log.Printf("Debug: VM %d - Interface %s has no ip-addresses field", id, ifaceName)
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
func (pm *ProxmoxManager) filterIPv4(output string) (string, error) {
	ips := strings.Fields(strings.TrimSpace(output))
	for _, ip := range ips {
		if net.ParseIP(ip) != nil && strings.HasPrefix(ip, pm.config.IPPrefix) {
			return ip, nil
		}
	}
	return "", fmt.Errorf("IP filtering: %w with prefix %s in output: %s", ErrNoIPFound, pm.config.IPPrefix, strings.TrimSpace(output))
}

// GetInstanceByIdentifier retrieves a Proxmox instance by its identifier.
// The identifier can be either the instance ID (as string) or the instance name.
// Returns the instance and a boolean indicating if it was found.
func (pm *ProxmoxManager) GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool) {
	value, exists := pm.instances.Load(identifier)
	if !exists {
		return ProxmoxInstance{}, false
	}
	instance, ok := value.(ProxmoxInstance)
	return instance, ok
}
