// Package config provides configuration structures and validation
// for the Proxmox DNS server application.
//
// This package defines ServerConfig for DNS server settings and
// ProxmoxConfig for Proxmox VE interaction parameters.
package config

import (
	"errors"
	"fmt"
	"time"
)

// Application-level error variables
var (
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrInvalidConfig        = errors.New("invalid server configuration")
	ErrInvalidProxmoxConfig = errors.New("invalid proxmox configuration")
)

// ServerConfig holds configuration parameters for the DNS server.
// It defines the zone to serve, network binding settings, and operational parameters.
type ServerConfig struct {
	Zone            string        // DNS zone to serve (e.g., "example.com")
	Port            string        // Port to listen on (e.g., "53")
	BindInterface   string        // Network interface to bind to (empty for all interfaces)
	IPPrefix        string        // IP prefix filter for container/VM IPs (e.g., "192.168.")
	RefreshInterval time.Duration // How often to refresh instance data from Proxmox
	DebugMode       bool          // Enable debug logging
}

// Validate checks if the ServerConfig contains valid configuration values.
// It returns an error if any required field is empty or invalid.
func (sc *ServerConfig) Validate() error {
	if sc.Zone == "" {
		return fmt.Errorf("%w: zone is required", ErrInvalidConfig)
	}

	if sc.Port == "" {
		return fmt.Errorf("%w: port is required", ErrInvalidConfig)
	}

	if sc.IPPrefix == "" {
		return fmt.Errorf("%w: IP prefix is required", ErrInvalidConfig)
	}

	if sc.RefreshInterval <= 0 {
		return fmt.Errorf("%w: refresh interval must be positive", ErrInvalidConfig)
	}

	return nil
}

// NewServerConfigFromFlags creates a ServerConfig from command line flag values.
// It sets default values for RefreshInterval and DebugMode.
func NewServerConfigFromFlags(zone, port, bindInterface, ipPrefix string) *ServerConfig {
	return &ServerConfig{
		Zone:            zone,
		Port:            port,
		BindInterface:   bindInterface,
		IPPrefix:        ipPrefix,
		RefreshInterval: 30 * time.Second, // Default refresh interval
		DebugMode:       false,            // Default debug mode
	}
}

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
		DebugMode:      false,            // Default debug mode
	}
}