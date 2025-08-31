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
			wantErr: true,
			errMsg:  "invalid server configuration: refresh interval must be positive",
		},
		{
			name: "Invalid refresh interval - negative",
			config: ServerConfig{
				Zone:            "example.com",
				Port:            "53",
				IPPrefix:        "192.168.",
				RefreshInterval: -1,
			},
			wantErr: true,
			errMsg:  "invalid server configuration: refresh interval must be positive",
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
		name     string
		ipPrefix string
		expected *ProxmoxConfig
	}{
		{
			name:     "Valid IP prefix",
			ipPrefix: "192.168.",
			expected: &ProxmoxConfig{
				IPPrefix:       "192.168.",
				CommandTimeout: 30 * time.Second,
				DebugMode:      false,
			},
		},
		{
			name:     "Different IP prefix",
			ipPrefix: "10.0.",
			expected: &ProxmoxConfig{
				IPPrefix:       "10.0.",
				CommandTimeout: 30 * time.Second,
				DebugMode:      false,
			},
		},
		{
			name:     "Empty IP prefix",
			ipPrefix: "",
			expected: &ProxmoxConfig{
				IPPrefix:       "",
				CommandTimeout: 30 * time.Second,
				DebugMode:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewProxmoxConfigFromFlags(tt.ipPrefix)
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
			name: "Valid configuration",
			config: ProxmoxConfig{
				IPPrefix:       "192.168.",
				CommandTimeout: 30 * time.Second,
				DebugMode:      false,
			},
			wantErr: false,
		},
		{
			name: "Missing IP prefix",
			config: ProxmoxConfig{
				IPPrefix:       "",
				CommandTimeout: 30 * time.Second,
				DebugMode:      false,
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: IP prefix is required",
		},
		{
			name: "Invalid timeout - zero",
			config: ProxmoxConfig{
				IPPrefix:       "192.168.",
				CommandTimeout: 0,
				DebugMode:      false,
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: command timeout must be positive",
		},
		{
			name: "Invalid timeout - negative",
			config: ProxmoxConfig{
				IPPrefix:       "192.168.",
				CommandTimeout: -1 * time.Second,
				DebugMode:      false,
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: command timeout must be positive",
		},
		{
			name: "Valid with debug mode",
			config: ProxmoxConfig{
				IPPrefix:       "10.0.",
				CommandTimeout: 60 * time.Second,
				DebugMode:      true,
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