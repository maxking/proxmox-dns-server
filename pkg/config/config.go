// Package config provides configuration structures and validation
// for the Proxmox DNS server application.
//
// This package defines ServerConfig for DNS server settings and
// ProxmoxConfig for Proxmox VE interaction parameters.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Application-level error variables
var (
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrInvalidConfig        = errors.New("invalid server configuration")
	ErrInvalidProxmoxConfig = errors.New("invalid proxmox configuration")
)

// Config represents the overall application configuration, combining server
// and Proxmox settings. It can be loaded from a JSON file.
type Config struct {
	Server  ServerConfig  `json:"server"`
	Proxmox ProxmoxConfig `json:"proxmox"`
}

// ServerConfig holds configuration parameters for the DNS server.
// It defines the zone to serve, network binding settings, and operational parameters.
type ServerConfig struct {
	Zone            string        `json:"zone"`
	Port            string        `json:"port"`
	BindInterface   string        `json:"interface"`
	Upstream        string        `json:"upstream"`
	IPPrefix        string        `json:"ip_prefix"`
	RefreshInterval time.Duration `json:"refresh_interval"`
	DebugMode       bool          `json:"debug"`
}

// ProxmoxConfig holds configuration parameters for Proxmox operations.
// It defines how the system interacts with Proxmox VE containers and VMs.
type ProxmoxConfig struct {
	APIEndpoint    string        `json:"api_endpoint"`
	Username       string        `json:"username"`
	Password       string        `json:"password"`
	APIToken       string        `json:"api_token"`
	APISecret      string        `json:"api_secret"`
	NodeName       string        `json:"node"`
	InsecureTLS    bool          `json:"insecure_tls"`
	IPPrefix       string        `json:"ip_prefix"`
	CommandTimeout time.Duration `json:"command_timeout"`
	DebugMode      bool          `json:"debug"`
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
		sc.RefreshInterval = 30 * time.Second
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

// Validate checks if the ProxmoxConfig contains valid configuration values.
// It returns an error if any required field is empty or invalid.
func (pc *ProxmoxConfig) Validate() error {
	if pc.APIEndpoint == "" {
		return fmt.Errorf("%w: api-endpoint is required", ErrInvalidProxmoxConfig)
	}

	if pc.Username == "" && pc.APIToken == "" {
		return fmt.Errorf("%w: either username or api-token is required", ErrInvalidProxmoxConfig)
	}

	if pc.Username != "" && pc.Password == "" {
		return fmt.Errorf("%w: password is required when username is provided", ErrInvalidProxmoxConfig)
	}

	if pc.APIToken != "" && pc.APISecret == "" {
		return fmt.Errorf("%w: api-secret is required when api-token is provided", ErrInvalidProxmoxConfig)
	}

	if pc.IPPrefix == "" {
		return fmt.Errorf("%w: IP prefix is required", ErrInvalidProxmoxConfig)
	}

	if pc.CommandTimeout <= 0 {
		pc.CommandTimeout = 30 * time.Second
	}

	return nil
}

// NewProxmoxConfigFromFlags creates a ProxmoxConfig with the specified IP prefix
// and default values for timeout and debug mode.
func NewProxmoxConfigFromFlags(ipPrefix, apiEndpoint, username, password, apiToken, apiSecret, nodeName string, insecureTLS bool) *ProxmoxConfig {
	return &ProxmoxConfig{
		IPPrefix:       ipPrefix,
		APIEndpoint:    apiEndpoint,
		Username:       username,
		Password:       password,
		APIToken:       apiToken,
		APISecret:      apiSecret,
		NodeName:       nodeName,
		InsecureTLS:    insecureTLS,
		CommandTimeout: 30 * time.Second, // Default command timeout
		DebugMode:      false,            // Default debug mode
	}
}

// LoadConfig reads a JSON configuration file from the given path and returns
// a Config struct.
func LoadConfig(path string) (*Config, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}
