// Package main implements a DNS server for Proxmox VE environments
// that resolves container and VM names to their IP addresses.
//
// The server automatically discovers Proxmox containers and VMs,
// caches their information, and provides DNS A record responses
// for queries matching the configured zone.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luthermonson/go-proxmox"
)

// Standard error variables for common error conditions
var (
	ErrContainerNotFound    = errors.New("container not found")
	ErrVMNotFound           = errors.New("VM not found")
	ErrInvalidID            = errors.New("invalid ID")
	ErrCommandTimeout       = errors.New("command execution timeout")
	ErrNoIPFound            = errors.New("no suitable IP address found")
	ErrInvalidIPPrefix      = errors.New("IP does not match configured prefix")
	ErrInvalidProxmoxConfig = errors.New("invalid proxmox configuration")
)

// ProxmoxConfig holds configuration parameters for Proxmox operations.
// It defines how the system interacts with Proxmox VE containers and VMs via API.
type ProxmoxConfig struct {
	IPPrefix    string // IP prefix filter for container/VM IPs (e.g., "192.168.")
	APIEndpoint string // Proxmox API endpoint (e.g., "https://proxmox:8006/api2/json")
	Username    string // Proxmox username (e.g., "root@pam")
	Password    string // Proxmox password
	APIToken    string // Alternative: API token (optional)
	APISecret   string // Alternative: API secret (optional)
	NodeName    string // Proxmox node name to query
	InsecureTLS bool   // Skip TLS verification for self-signed certs
	DebugMode   bool   // Enable debug logging for Proxmox operations
}

// Validate checks if the ProxmoxConfig contains valid configuration values.
// It returns an error if any required field is empty or invalid.
func (pc *ProxmoxConfig) Validate() error {
	if pc.IPPrefix == "" {
		return fmt.Errorf("%w: IP prefix is required", ErrInvalidProxmoxConfig)
	}

	if pc.APIEndpoint == "" {
		return fmt.Errorf("%w: API endpoint is required", ErrInvalidProxmoxConfig)
	}

	if pc.NodeName == "" {
		return fmt.Errorf("%w: node name is required", ErrInvalidProxmoxConfig)
	}

	// Either username/password or API token/secret must be provided
	if pc.Username == "" && pc.APIToken == "" {
		return fmt.Errorf("%w: either username or API token is required", ErrInvalidProxmoxConfig)
	}

	if pc.Username != "" && pc.Password == "" {
		return fmt.Errorf("%w: password is required when username is provided", ErrInvalidProxmoxConfig)
	}

	if pc.APIToken != "" && pc.APISecret == "" {
		return fmt.Errorf("%w: API secret is required when API token is provided", ErrInvalidProxmoxConfig)
	}

	return nil
}

// NewProxmoxConfigFromFlags creates a ProxmoxConfig with the specified parameters
// and default values for debug mode and TLS settings.
func NewProxmoxConfigFromFlags(ipPrefix, apiEndpoint, username, password, apiToken, apiSecret, nodeName string, insecureTLS bool) *ProxmoxConfig {
	return &ProxmoxConfig{
		IPPrefix:    ipPrefix,
		APIEndpoint: apiEndpoint,
		Username:    username,
		Password:    password,
		APIToken:    apiToken,
		APISecret:   apiSecret,
		NodeName:    nodeName,
		InsecureTLS: insecureTLS,
		DebugMode:   false, // Default debug mode
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
// It provides thread-safe access to instance data and handles periodic refreshes via API.
type ProxmoxManager struct {
	instances sync.Map           // Thread-safe map storing instances by ID and name
	config    ProxmoxConfig      // Configuration for Proxmox operations
	client    *proxmox.Client    // Proxmox API client
}

// Proxmox ID validation constants
const (
	MinProxmoxID = 100       // Proxmox typical minimum ID
	MaxProxmoxID = 999999999 // Proxmox theoretical maximum ID (9 digits)
)

// NewProxmoxManager creates a new ProxmoxManager with the given configuration.
// It initializes the thread-safe instance storage and creates the API client.
func NewProxmoxManager(config ProxmoxConfig) *ProxmoxManager {
	// Create HTTP client with optional TLS configuration
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: config.InsecureTLS,
			},
		},
	}

	// Create Proxmox client with authentication
	var client *proxmox.Client
	if config.APIToken != "" {
		// Use API token authentication
		client = proxmox.NewClient(config.APIEndpoint,
			proxmox.WithAPIToken(config.APIToken, config.APISecret),
			proxmox.WithHTTPClient(httpClient),
		)
	} else {
		// Use username/password authentication
		credentials := proxmox.Credentials{
			Username: config.Username,
			Password: config.Password,
		}
		client = proxmox.NewClient(config.APIEndpoint,
			proxmox.WithCredentials(&credentials),
			proxmox.WithHTTPClient(httpClient),
		)
	}

	return &ProxmoxManager{
		config: config,
		client: client,
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

// loadContainers discovers and loads all Proxmox containers via API.
// It retrieves container information and IP addresses for running containers
// that match the configured IP prefix.
func (pm *ProxmoxManager) loadContainers() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the node first
	node, err := pm.client.Node(ctx, pm.config.NodeName)
	if err != nil {
		return fmt.Errorf("proxmox containers: failed to get node %s: %w", pm.config.NodeName, err)
	}

	// Get containers from the specified node
	containers, err := node.Containers(ctx)
	if err != nil {
		return fmt.Errorf("proxmox containers: failed to get containers from API: %w", err)
	}

	if pm.config.DebugMode {
		log.Printf("Debug: Found %d containers on node %s", len(containers), pm.config.NodeName)
	}

	for _, container := range containers {
		// Only process running containers
		if container.Status != "running" {
			if pm.config.DebugMode {
				log.Printf("Debug: Container %d (%s) is %s, skipping IP detection", container.VMID, container.Name, container.Status)
			}
			continue
		}

		containerID := int(uint64(container.VMID))
		
		ipv4, err := pm.getContainerIP(containerID)
		if err != nil {
			log.Printf("Warning: Failed to get IP for container %d (%s): %v", containerID, container.Name, err)
			continue
		}

		instance := ProxmoxInstance{
			ID:     containerID,
			Name:   container.Name,
			Status: container.Status,
			Type:   "container",
			IPv4:   ipv4,
		}

		pm.instances.Store(strconv.Itoa(containerID), instance)
		pm.instances.Store(container.Name, instance)

		if pm.config.DebugMode {
			log.Printf("Debug: Loaded container %d (%s) with IP %s", containerID, container.Name, ipv4)
		}
	}

	return nil
}

// loadVMs discovers and loads all Proxmox virtual machines via API.
// It retrieves VM information and IP addresses for running VMs
// that match the configured IP prefix.
func (pm *ProxmoxManager) loadVMs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the node first
	node, err := pm.client.Node(ctx, pm.config.NodeName)
	if err != nil {
		return fmt.Errorf("proxmox VMs: failed to get node %s: %w", pm.config.NodeName, err)
	}

	// Get VMs from the specified node
	vms, err := node.VirtualMachines(ctx)
	if err != nil {
		return fmt.Errorf("proxmox VMs: failed to get VMs from API: %w", err)
	}

	if pm.config.DebugMode {
		log.Printf("Debug: Found %d VMs on node %s", len(vms), pm.config.NodeName)
	}

	for _, vm := range vms {
		// Only process running VMs
		if vm.Status != "running" {
			if pm.config.DebugMode {
				log.Printf("Debug: VM %d (%s) is %s, skipping IP detection", vm.VMID, vm.Name, vm.Status)
			}
			continue
		}

		vmID := int(uint64(vm.VMID))
		
		ipv4, err := pm.getVMIP(vmID)
		if err != nil {
			log.Printf("Warning: Failed to get IP for VM %d (%s): %v", vmID, vm.Name, err)
			continue
		}

		instance := ProxmoxInstance{
			ID:     vmID,
			Name:   vm.Name,
			Status: vm.Status,
			Type:   "vm",
			IPv4:   ipv4,
		}

		pm.instances.Store(strconv.Itoa(vmID), instance)
		pm.instances.Store(vm.Name, instance)

		if pm.config.DebugMode {
			log.Printf("Debug: Loaded VM %d (%s) with IP %s", vmID, vm.Name, ipv4)
		}
	}

	return nil
}

// getContainerIP retrieves the IP address of a Proxmox container by ID via API.
// It gets the container configuration and extracts IP addresses from network interfaces.
// Returns the first IP address that matches the configured prefix.
func (pm *ProxmoxManager) getContainerIP(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("container IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the node first
	node, err := pm.client.Node(ctx, pm.config.NodeName)
	if err != nil {
		return "", fmt.Errorf("container IP lookup: failed to get node %s: %w", pm.config.NodeName, err)
	}

	// Get the container (for future implementation)
	_, err = node.Container(ctx, id)
	if err != nil {
		return "", fmt.Errorf("container %d: failed to get container from API: %w", id, err)
	}

	// For now, return an error indicating that container IP detection via API is not yet implemented
	// This can be enhanced later with proper container configuration API integration
	if pm.config.DebugMode {
		log.Printf("Debug: Container %d - container IP detection via API not fully implemented yet", id)
	}
	
	return "", fmt.Errorf("container %d: IP detection via API not yet implemented", id)
}

// extractIPFromNetConfig extracts IP address from network configuration string.
// Network config format: "name=eth0,bridge=vmbr0,ip=192.168.1.100/24,gw=192.168.1.1"
func (pm *ProxmoxManager) extractIPFromNetConfig(netConfig string) string {
	// Find "ip=" in the configuration
	ipStart := strings.Index(netConfig, "ip=")
	if ipStart == -1 {
		return ""
	}
	
	ipStart += 3 // Skip "ip="
	
	// Find the end of the IP (either a comma, slash, or end of string)
	ipEnd := strings.IndexAny(netConfig[ipStart:], ",/")
	if ipEnd == -1 {
		ipEnd = len(netConfig) - ipStart
	}
	
	return netConfig[ipStart : ipStart+ipEnd]
}


// getVMIP retrieves the IP address of a Proxmox virtual machine by ID via AgentExec API.
// It uses the QEMU guest agent to execute commands and extract IP information
// and returns the first IPv4 address matching the configured prefix.
func (pm *ProxmoxManager) getVMIP(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("VM IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the node first
	node, err := pm.client.Node(ctx, pm.config.NodeName)
	if err != nil {
		return "", fmt.Errorf("VM IP lookup: failed to get node %s: %w", pm.config.NodeName, err)
	}

	// Get the virtual machine (for future implementation)
	_, err = node.VirtualMachine(ctx, id)
	if err != nil {
		return "", fmt.Errorf("VM %d: failed to get VM from API: %w", id, err)
	}

	// For now, return an error indicating that VM IP detection via API is not yet implemented
	// This can be enhanced later with proper QEMU guest agent integration
	if pm.config.DebugMode {
		log.Printf("Debug: VM %d - VM IP detection via API not fully implemented yet", id)
	}
	
	return "", fmt.Errorf("VM %d: IP detection via API not yet implemented", id)
}

// filterIPv4 parses command output to find valid IPv4 addresses.
// It returns the first IP address that matches the configured prefix.
func (pm *ProxmoxManager) filterIPv4(output string) (string, error) {
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
func (pm *ProxmoxManager) GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool) {
	value, exists := pm.instances.Load(identifier)
	if !exists {
		return ProxmoxInstance{}, false
	}
	instance, ok := value.(ProxmoxInstance)
	return instance, ok
}
