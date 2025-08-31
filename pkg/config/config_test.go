package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewServerConfigFromFlags(t *testing.T) {
	tests := []struct {
		name          string
		zone          string
		port          string
		bindInterface string
		ipPrefix      string
		expected      *ServerConfig
	}{
		{
			name:          "Valid configuration",
			zone:          "example.com",
			port:          "53",
			bindInterface: "eth0",
			ipPrefix:      "192.168.",
			expected: &ServerConfig{
				Zone:            "example.com",
				Port:            "53",
				BindInterface:   "eth0",
				IPPrefix:        "192.168.",
				RefreshInterval: 30 * time.Second,
				DebugMode:       false,
			},
		},
		{
			name:          "Minimal configuration",
			zone:          "test.local",
			port:          "5353",
			bindInterface: "",
			ipPrefix:      "10.0.",
			expected: &ServerConfig{
				Zone:            "test.local",
				Port:            "5353",
				BindInterface:   "",
				IPPrefix:        "10.0.",
				RefreshInterval: 30 * time.Second,
				DebugMode:       false,
			},
		},
		{
			name:          "Empty strings handled",
			zone:          "",
			port:          "",
			bindInterface: "",
			ipPrefix:      "",
			expected: &ServerConfig{
				Zone:            "",
				Port:            "",
				BindInterface:   "",
				IPPrefix:        "",
				RefreshInterval: 30 * time.Second,
				DebugMode:       false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewServerConfigFromFlags(tt.zone, tt.port, tt.bindInterface, tt.ipPrefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid configuration",
			config: ServerConfig{
				Zone:            "example.com",
				Port:            "53",
				IPPrefix:        "192.168.",
				RefreshInterval: 30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Missing zone",
			config: ServerConfig{
				Zone:            "",
				Port:            "53",
				IPPrefix:        "192.168.",
				RefreshInterval: 30 * time.Second,
			},
			wantErr: true,
			errMsg:  "invalid server configuration: zone is required",
		},
		{
			name: "Missing port",
			config: ServerConfig{
				Zone:            "example.com",
				Port:            "",
				IPPrefix:        "192.168.",
				RefreshInterval: 30 * time.Second,
			},
			wantErr: true,
			errMsg:  "invalid server configuration: port is required",
		},
		{
			name: "Missing IP prefix",
			config: ServerConfig{
				Zone:            "example.com",
				Port:            "53",
				IPPrefix:        "",
				RefreshInterval: 30 * time.Second,
			},
			wantErr: true,
			errMsg:  "invalid server configuration: IP prefix is required",
		},
		{
			name: "Invalid refresh interval - zero",
			config: ServerConfig{
				Zone:            "example.com",
				Port:            "53",
				IPPrefix:        "192.168.",
				RefreshInterval: 0,
			},
			wantErr: false, // Now defaults to 30s
		},
		{
			name: "Invalid refresh interval - negative",
			config: ServerConfig{
				Zone:            "example.com",
				Port:            "53",
				IPPrefix:        "192.168.",
				RefreshInterval: -1,
			},
			wantErr: false, // Now defaults to 30s
		},
		{
			name: "Valid with bind interface",
			config: ServerConfig{
				Zone:            "test.local",
				Port:            "5353",
				BindInterface:   "eth0",
				IPPrefix:        "10.0.",
				RefreshInterval: 60 * time.Second,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.errMsg, err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewProxmoxConfigFromFlags(t *testing.T) {
	tests := []struct {
		name        string
		ipPrefix    string
		apiEndpoint string
		username    string
		password    string
		apiToken    string
		apiSecret   string
		nodeName    string
		insecureTLS bool
		expected    *ProxmoxConfig
	}{
		{
			name:        "Valid Proxmox configuration",
			ipPrefix:    "192.168.",
			apiEndpoint: "https://proxmox:8006/api2/json",
			username:    "root@pam",
			password:    "secret",
			nodeName:    "pve",
			insecureTLS: true,
			expected: &ProxmoxConfig{
				IPPrefix:       "192.168.",
				APIEndpoint:    "https://proxmox:8006/api2/json",
				Username:       "root@pam",
				Password:       "secret",
				NodeName:       "pve",
				InsecureTLS:    true,
				CommandTimeout: 30 * time.Second,
				DebugMode:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewProxmoxConfigFromFlags(tt.ipPrefix, tt.apiEndpoint, tt.username, tt.password, tt.apiToken, tt.apiSecret, tt.nodeName, tt.insecureTLS)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProxmoxConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ProxmoxConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid configuration with user/pass",
			config: ProxmoxConfig{
				IPPrefix:       "192.168.",
				APIEndpoint:    "https://proxmox:8006/api2/json",
				Username:       "root@pam",
				Password:       "secret",
				CommandTimeout: 30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Valid configuration with api token",
			config: ProxmoxConfig{
				IPPrefix:       "192.168.",
				APIEndpoint:    "https://proxmox:8006/api2/json",
				APIToken:       "root@pam!token",
				APISecret:      "secret",
				CommandTimeout: 30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Missing IP prefix",
			config: ProxmoxConfig{
				APIEndpoint:    "https://proxmox:8006/api2/json",
				Username:       "root@pam",
				Password:       "secret",
				CommandTimeout: 30 * time.Second,
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: IP prefix is required",
		},
		{
			name: "Invalid timeout - zero",
			config: ProxmoxConfig{
				IPPrefix:       "192.168.",
				APIEndpoint:    "https://proxmox:8006/api2/json",
				Username:       "root@pam",
				Password:       "secret",
				CommandTimeout: 0,
			},
			wantErr: false, // Now defaults to 30s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.errMsg, err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestErrorConstants ensures our error constants are defined correctly
func TestErrorConstants(t *testing.T) {
	assert.NotEmpty(t, ErrInvalidConfiguration.Error())
	assert.NotEmpty(t, ErrInvalidConfig.Error())
	assert.NotEmpty(t, ErrInvalidProxmoxConfig.Error())

	assert.Equal(t, "invalid configuration", ErrInvalidConfiguration.Error())
	assert.Equal(t, "invalid server configuration", ErrInvalidConfig.Error())
	assert.Equal(t, "invalid proxmox configuration", ErrInvalidProxmoxConfig.Error())
}

// BenchmarkNewServerConfigFromFlags benchmarks the configuration creation
func BenchmarkNewServerConfigFromFlags(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewServerConfigFromFlags("example.com", "53", "eth0", "192.168.")
	}
}

// BenchmarkServerConfigValidate benchmarks configuration validation
func BenchmarkServerConfigValidate(b *testing.B) {
	config := ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.Validate()
	}
}
