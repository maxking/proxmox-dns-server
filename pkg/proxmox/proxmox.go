// Package main implements a DNS server for Proxmox VE environments
// that resolves container and VM names to their IP addresses.
//
// The server automatically discovers Proxmox containers and VMs,
// caches their information, and provides DNS A record responses
// for queries matching the configured zone.
package proxmox

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luthermonson/go-proxmox"
	"go.uber.org/zap"
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

// ProxmoxManager manages the discovery and caching of Proxmox container and VM instances.
// It provides thread-safe access to instance data and handles periodic refreshes via API.
type ProxmoxManager struct {
	instances sync.Map             // Thread-safe map storing instances by ID and name
	config    config.ProxmoxConfig // Configuration for Proxmox operations
	client    *proxmox.Client      // Proxmox API client
	logger    *zap.Logger          // Logger for logging messages
}

// Proxmox ID validation constants
const (
	MinProxmoxID = 100       // Proxmox typical minimum ID
	MaxProxmoxID = 999999999 // Proxmox theoretical maximum ID (9 digits)
)

// NewProxmoxManager creates a new ProxmoxManager with the given configuration.
// It initializes the thread-safe instance storage and creates the API client.
func NewProxmoxManager(config config.ProxmoxConfig, logger *zap.Logger) *ProxmoxManager {
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
		logger: logger,
	}
}

// getNodes retrieves all nodes in the Proxmox cluster.
// If NodeName is specified in config, returns only that node.
// If NodeName is empty, returns all online nodes in the cluster.
func (pm *ProxmoxManager) getNodes(ctx context.Context) ([]string, error) {
	// If a specific node is configured, return only that node
	if pm.config.NodeName != "" {
		return []string{pm.config.NodeName}, nil
	}

	// Get all nodes in the cluster
	nodes, err := pm.client.Nodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	var nodeNames []string
	for _, node := range nodes {
		// Only include online nodes
		if node.Status == "online" {
			nodeNames = append(nodeNames, node.Node)
			if pm.config.DebugMode {
				pm.logger.Debug("Found online node", zap.String("node", node.Node))
			}
		} else if pm.config.DebugMode {
			pm.logger.Debug("Skipping offline node",
				zap.String("node", node.Node),
				zap.String("status", node.Status),
			)
		}
	}

	if len(nodeNames) == 0 {
		return nil, fmt.Errorf("no online nodes found in cluster")
	}

	return nodeNames, nil
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
// that match the configured IP prefix from all specified nodes.
func (pm *ProxmoxManager) loadContainers() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get all nodes to query (either specific node or all cluster nodes)
	nodeNames, err := pm.getNodes(ctx)
	if err != nil {
		return fmt.Errorf("proxmox containers: %w", err)
	}

	totalContainers := 0
	for _, nodeName := range nodeNames {
		// Get the node
		node, err := pm.client.Node(ctx, nodeName)
		if err != nil {
			pm.logger.Warn("Failed to get node",
				zap.String("node", nodeName),
				zap.Error(err),
			)
			continue
		}

		// Get containers from this node
		containers, err := node.Containers(ctx)
		if err != nil {
			pm.logger.Warn("Failed to get containers from node",
				zap.String("node", nodeName),
				zap.Error(err),
			)
			continue
		}

		if pm.config.DebugMode {
			pm.logger.Debug("Found containers on node",
				zap.Int("count", len(containers)),
				zap.String("node", nodeName),
			)
		}
		totalContainers += len(containers)

		for _, container := range containers {
			// Only process running containers
			if container.Status != "running" {
				if pm.config.DebugMode {
					pm.logger.Debug("Container is not running, skipping IP detection",
						zap.Int("id", int(uint64(container.VMID))),
						zap.String("name", container.Name),
						zap.String("node", nodeName),
						zap.String("status", container.Status),
					)
				}
				continue
			}

			containerID := int(uint64(container.VMID))

			ipv4, err := pm.getContainerIPFromNode(containerID, nodeName)
			if err != nil {
				pm.logger.Warn("Failed to get IP for container",
					zap.Int("id", containerID),
					zap.String("name", container.Name),
					zap.String("node", nodeName),
					zap.Error(err),
				)
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
				pm.logger.Debug("Loaded container",
					zap.Int("id", containerID),
					zap.String("name", container.Name),
					zap.String("node", nodeName),
					zap.String("ip", ipv4),
				)
			}
		}
	}

	if pm.config.DebugMode {
		pm.logger.Debug("Total containers found", zap.Int("count", totalContainers))
	}

	return nil
}

// loadVMs discovers and loads all Proxmox virtual machines via API.
// It retrieves VM information and IP addresses for running VMs
// that match the configured IP prefix from all specified nodes.
func (pm *ProxmoxManager) loadVMs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get all nodes to query (either specific node or all cluster nodes)
	nodeNames, err := pm.getNodes(ctx)
	if err != nil {
		return fmt.Errorf("proxmox VMs: %w", err)
	}

	totalVMs := 0
	for _, nodeName := range nodeNames {
		// Get the node
		node, err := pm.client.Node(ctx, nodeName)
		if err != nil {
			pm.logger.Warn("Failed to get node",
				zap.String("node", nodeName),
				zap.Error(err),
			)
			continue
		}

		// Get VMs from this node
		vms, err := node.VirtualMachines(ctx)
		if err != nil {
			pm.logger.Warn("Failed to get VMs from node",
				zap.String("node", nodeName),
				zap.Error(err),
			)
			continue
		}

		if pm.config.DebugMode {
			pm.logger.Debug("Found VMs on node",
				zap.Int("count", len(vms)),
				zap.String("node", nodeName),
			)
		}
		totalVMs += len(vms)

		for _, vm := range vms {
			// Only process running VMs
			if vm.Status != "running" {
				if pm.config.DebugMode {
					pm.logger.Debug("VM is not running, skipping IP detection",
						zap.Int("id", int(uint64(vm.VMID))),
						zap.String("name", vm.Name),
						zap.String("node", nodeName),
						zap.String("status", vm.Status),
					)
				}
				continue
			}

			vmID := int(uint64(vm.VMID))

			ipv4, err := pm.getVMIPFromNode(vmID, nodeName)
			if err != nil {
				pm.logger.Warn("Failed to get IP for VM",
					zap.Int("id", vmID),
					zap.String("name", vm.Name),
					zap.String("node", nodeName),
					zap.Error(err),
				)
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
				pm.logger.Debug("Loaded VM",
					zap.Int("id", vmID),
					zap.String("name", vm.Name),
					zap.String("node", nodeName),
					zap.String("ip", ipv4),
				)
			}
		}
	}

	if pm.config.DebugMode {
		pm.logger.Debug("Total VMs found", zap.Int("count", totalVMs))
	}

	return nil
}

// getContainerIP retrieves the IP address of a Proxmox container by ID via API.
// It gets the container configuration and extracts IP addresses from network interfaces.
// Returns the first IP address that matches the configured prefix.
// If NodeName is configured, searches only that node. Otherwise, searches all cluster nodes.
func (pm *ProxmoxManager) getContainerIP(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("container IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}

	// If a specific node is configured, use that node directly
	if pm.config.NodeName != "" {
		return pm.getContainerIPFromNode(id, pm.config.NodeName)
	}

	// Otherwise, search all cluster nodes
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nodeNames, err := pm.getNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("container IP lookup: %w", err)
	}

	// Try each node until we find the container
	for _, nodeName := range nodeNames {
		ip, err := pm.getContainerIPFromNode(id, nodeName)
		if err == nil {
			return ip, nil
		}

		// Log the error but continue to next node (container might be on a different node)
		if pm.config.DebugMode {
			pm.logger.Debug("Container not found on node",
				zap.Int("id", id),
				zap.String("node", nodeName),
				zap.Error(err),
			)
		}
	}

	return "", fmt.Errorf("container %d: not found on any cluster node with prefix %s", id, pm.config.IPPrefix)
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


// getVMIP retrieves the IP address of a Proxmox virtual machine by ID via QEMU guest agent.
// It uses the QEMU guest agent to query network interfaces and extract IP information
// and returns the first IPv4 address matching the configured prefix.
// If NodeName is configured, searches only that node. Otherwise, searches all cluster nodes.
func (pm *ProxmoxManager) getVMIP(id int) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("VM IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}

	// If a specific node is configured, use that node directly
	if pm.config.NodeName != "" {
		return pm.getVMIPFromNode(id, pm.config.NodeName)
	}

	// Otherwise, search all cluster nodes
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nodeNames, err := pm.getNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("VM IP lookup: %w", err)
	}

	// Try each node until we find the VM
	for _, nodeName := range nodeNames {
		ip, err := pm.getVMIPFromNode(id, nodeName)
		if err == nil {
			return ip, nil
		}

		// Log the error but continue to next node (VM might be on a different node)
		if pm.config.DebugMode {
			pm.logger.Debug("VM not found on node",
				zap.Int("id", id),
				zap.String("node", nodeName),
				zap.Error(err),
			)
		}
	}

	return "", fmt.Errorf("VM %d: not found on any cluster node with prefix %s", id, pm.config.IPPrefix)
}

// getContainerIPFromNode retrieves the IP address of a Proxmox container by ID from a specific node.
// This is used when iterating through multiple nodes in the cluster.
func (pm *ProxmoxManager) getContainerIPFromNode(id int, nodeName string) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("container IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the specific node
	node, err := pm.client.Node(ctx, nodeName)
	if err != nil {
		return "", fmt.Errorf("container IP lookup: failed to get node %s: %w", nodeName, err)
	}

	// Get the container
	container, err := node.Container(ctx, id)
	if err != nil {
		return "", fmt.Errorf("container %d on node %s: failed to get container from API: %w", id, nodeName, err)
	}

	// Get container network interfaces
	interfaces, err := container.Interfaces(ctx)
	if err != nil {
		return "", fmt.Errorf("container %d on node %s: failed to get network interfaces: %w", id, nodeName, err)
	}

	// Look for the first IPv4 address that matches the configured prefix
	for _, iface := range interfaces {
		if iface.Inet != "" {
			// Parse the IP (might be in CIDR format like "192.168.1.100/24")
			ip := strings.Split(iface.Inet, "/")[0]

			// Validate it's a proper IP
			if net.ParseIP(ip) != nil && strings.HasPrefix(ip, pm.config.IPPrefix) {
				if pm.config.DebugMode {
					pm.logger.Debug("Found IP for container on node",
						zap.Int("id", id),
						zap.String("node", nodeName),
						zap.String("ip", ip),
						zap.String("interface", iface.Name),
					)
				}
				return ip, nil
			}
		}
	}

	return "", fmt.Errorf("container %d on node %s: %w with prefix %s", id, nodeName, ErrNoIPFound, pm.config.IPPrefix)
}

// getVMIPFromNode retrieves the IP address of a Proxmox virtual machine by ID from a specific node.
// This is used when iterating through multiple nodes in the cluster.
func (pm *ProxmoxManager) getVMIPFromNode(id int, nodeName string) (string, error) {
	if id < MinProxmoxID || id > MaxProxmoxID {
		return "", fmt.Errorf("VM IP lookup: %w: %d (must be between %d and %d)", ErrInvalidID, id, MinProxmoxID, MaxProxmoxID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the specific node
	node, err := pm.client.Node(ctx, nodeName)
	if err != nil {
		return "", fmt.Errorf("VM IP lookup: failed to get node %s: %w", nodeName, err)
	}

	// Get the virtual machine
	vm, err := node.VirtualMachine(ctx, id)
	if err != nil {
		return "", fmt.Errorf("VM %d on node %s: failed to get VM from API: %w", id, nodeName, err)
	}

	// Get network interfaces via guest agent
	interfaces, err := vm.AgentGetNetworkIFaces(ctx)
	if err != nil {
		return "", fmt.Errorf("VM %d on node %s: failed to get network interfaces via guest agent: %w", id, nodeName, err)
	}

	// Look for the first IPv4 address that matches the configured prefix
	for _, iface := range interfaces {
		if pm.config.DebugMode {
			pm.logger.Debug("Checking interface for VM",
				zap.Int("id", id),
				zap.String("node", nodeName),
				zap.String("interface", iface.Name),
			)
		}

		for _, ipAddr := range iface.IPAddresses {
			if ipAddr.IPAddressType == "ipv4" {
				ip := ipAddr.IPAddress

				// Validate it's a proper IP and matches prefix
				if net.ParseIP(ip) != nil && strings.HasPrefix(ip, pm.config.IPPrefix) {
					if pm.config.DebugMode {
						pm.logger.Debug("Found IP for VM on node",
							zap.Int("id", id),
							zap.String("node", nodeName),
							zap.String("ip", ip),
							zap.String("interface", iface.Name),
						)
					}
					return ip, nil
				}
			}
		}
	}

	return "", fmt.Errorf("VM %d on node %s: %w with prefix %s", id, nodeName, ErrNoIPFound, pm.config.IPPrefix)
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
