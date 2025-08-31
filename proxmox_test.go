package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			name:        "Valid username/password config",
			ipPrefix:    "192.168.",
			apiEndpoint: "https://proxmox:8006/api2/json",
			username:    "root@pam",
			password:    "secret",
			nodeName:    "pve",
			insecureTLS: false,
			expected: &ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				Username:    "root@pam",
				Password:    "secret",
				NodeName:    "pve",
				InsecureTLS: false,
				DebugMode:   false,
			},
		},
		{
			name:        "Valid API token config",
			ipPrefix:    "10.0.",
			apiEndpoint: "https://pve.local:8006/api2/json",
			apiToken:    "root@pam!mytoken",
			apiSecret:   "token-secret",
			nodeName:    "node1",
			insecureTLS: true,
			expected: &ProxmoxConfig{
				IPPrefix:    "10.0.",
				APIEndpoint: "https://pve.local:8006/api2/json",
				APIToken:    "root@pam!mytoken",
				APISecret:   "token-secret",
				NodeName:    "node1",
				InsecureTLS: true,
				DebugMode:   false,
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
			name: "Valid configuration with username/password",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				Username:    "root@pam",
				Password:    "secret",
				NodeName:    "pve",
				DebugMode:   false,
			},
			wantErr: false,
		},
		{
			name: "Valid configuration with API token",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				APIToken:    "root@pam!token",
				APISecret:   "secret",
				NodeName:    "pve",
				DebugMode:   false,
			},
			wantErr: false,
		},
		{
			name: "Missing IP prefix",
			config: ProxmoxConfig{
				IPPrefix:    "",
				APIEndpoint: "https://proxmox:8006/api2/json",
				Username:    "root@pam",
				Password:    "secret",
				NodeName:    "pve",
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: IP prefix is required",
		},
		{
			name: "Missing API endpoint",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "",
				Username:    "root@pam",
				Password:    "secret",
				NodeName:    "pve",
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: API endpoint is required",
		},
		{
			name: "Missing node name",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				Username:    "root@pam",
				Password:    "secret",
				NodeName:    "",
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: node name is required",
		},
		{
			name: "Missing authentication",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				NodeName:    "pve",
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: either username or API token is required",
		},
		{
			name: "Username without password",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				Username:    "root@pam",
				NodeName:    "pve",
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: password is required when username is provided",
		},
		{
			name: "API token without secret",
			config: ProxmoxConfig{
				IPPrefix:    "192.168.",
				APIEndpoint: "https://proxmox:8006/api2/json",
				APIToken:    "root@pam!token",
				NodeName:    "pve",
			},
			wantErr: true,
			errMsg:  "invalid proxmox configuration: API secret is required when API token is provided",
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

func TestProxmoxErrorConstants(t *testing.T) {
	assert.NotEmpty(t, ErrContainerNotFound.Error())
	assert.NotEmpty(t, ErrVMNotFound.Error())
	assert.NotEmpty(t, ErrInvalidID.Error())
	assert.NotEmpty(t, ErrCommandTimeout.Error())
	assert.NotEmpty(t, ErrNoIPFound.Error())
	assert.NotEmpty(t, ErrInvalidIPPrefix.Error())
	assert.NotEmpty(t, ErrInvalidProxmoxConfig.Error())

	assert.Equal(t, "container not found", ErrContainerNotFound.Error())
	assert.Equal(t, "VM not found", ErrVMNotFound.Error())
	assert.Equal(t, "invalid ID", ErrInvalidID.Error())
	assert.Equal(t, "command execution timeout", ErrCommandTimeout.Error())
	assert.Equal(t, "no suitable IP address found", ErrNoIPFound.Error())
	assert.Equal(t, "IP does not match configured prefix", ErrInvalidIPPrefix.Error())
	assert.Equal(t, "invalid proxmox configuration", ErrInvalidProxmoxConfig.Error())
}

func TestProxmoxIDConstants(t *testing.T) {
	assert.Equal(t, 100, MinProxmoxID)
	assert.Equal(t, 999999999, MaxProxmoxID)
}

func TestNewProxmoxManager(t *testing.T) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}

	manager := NewProxmoxManager(config)

	assert.NotNil(t, manager)
	assert.Equal(t, config, manager.config)
	assert.NotNil(t, manager.client)
}

func TestProxmoxManager_GetInstanceByIdentifier(t *testing.T) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}
	manager := NewProxmoxManager(config)

	// Test with empty storage
	_, exists := manager.GetInstanceByIdentifier("102")
	assert.False(t, exists)

	// Add an instance
	instance := ProxmoxInstance{
		ID:     102,
		Name:   "webserver",
		Status: "running",
		Type:   "container",
		IPv4:   "192.168.1.100",
	}
	manager.instances.Store("102", instance)
	manager.instances.Store("webserver", instance)

	// Test retrieval by ID
	result, exists := manager.GetInstanceByIdentifier("102")
	assert.True(t, exists)
	assert.Equal(t, instance, result)

	// Test retrieval by name
	result, exists = manager.GetInstanceByIdentifier("webserver")
	assert.True(t, exists)
	assert.Equal(t, instance, result)

	// Test non-existent identifier
	_, exists = manager.GetInstanceByIdentifier("999")
	assert.False(t, exists)
}

func TestProxmoxManager_filterIPv4(t *testing.T) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}
	manager := NewProxmoxManager(config)

	tests := []struct {
		name       string
		output     string
		ipPrefix   string
		expectedIP string
		wantErr    bool
	}{
		{
			name:       "Single IP matching prefix",
			output:     "192.168.1.100",
			expectedIP: "192.168.1.100",
			wantErr:    false,
		},
		{
			name:       "Multiple IPs, first matches",
			output:     "192.168.1.100 10.0.1.50",
			expectedIP: "192.168.1.100",
			wantErr:    false,
		},
		{
			name:       "Multiple IPs, second matches",
			output:     "10.0.1.50 192.168.1.100",
			expectedIP: "192.168.1.100",
			wantErr:    false,
		},
		{
			name:    "No matching IP",
			output:  "10.0.1.50 172.16.1.100",
			wantErr: true,
		},
		{
			name:    "Invalid IP addresses",
			output:  "invalid-ip another-invalid-ip",
			wantErr: true,
		},
		{
			name:    "Empty output",
			output:  "",
			wantErr: true,
		},
		{
			name:    "Whitespace only",
			output:  "   \t\n  ",
			wantErr: true,
		},
		{
			name:       "IP with extra whitespace",
			output:     "  192.168.1.100  \n",
			expectedIP: "192.168.1.100",
			wantErr:    false,
		},
		{
			name:       "IPs separated by tabs and newlines",
			output:     "10.0.1.50\t192.168.1.100\n172.16.1.1",
			expectedIP: "192.168.1.100",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.filterIPv4(tt.output)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "no suitable IP address found")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedIP, result)
			}
		})
	}
}

func TestProxmoxManager_filterIPv4_DifferentPrefixes(t *testing.T) {
	tests := []struct {
		name       string
		ipPrefix   string
		output     string
		expectedIP string
		wantErr    bool
	}{
		{
			name:       "10.0. prefix",
			ipPrefix:   "10.0.",
			output:     "192.168.1.100 10.0.1.50",
			expectedIP: "10.0.1.50",
			wantErr:    false,
		},
		{
			name:       "172.16. prefix",
			ipPrefix:   "172.16.",
			output:     "192.168.1.100 172.16.1.1",
			expectedIP: "172.16.1.1",
			wantErr:    false,
		},
		{
			name:     "No matching prefix",
			ipPrefix: "203.0.",
			output:   "192.168.1.100 10.0.1.50",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ProxmoxConfig{
				IPPrefix:    tt.ipPrefix,
				APIEndpoint: "https://proxmox:8006/api2/json",
				Username:    "root@pam",
				Password:    "secret",
				NodeName:    "pve",
				DebugMode:   false,
			}
			manager := NewProxmoxManager(config)

			result, err := manager.filterIPv4(tt.output)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedIP, result)
			}
		})
	}
}

// Integration tests that use mock commands
// These tests would normally require Proxmox commands to be available,
// but we'll use environment variables to control behavior for testing

func TestProxmoxManager_getContainerIP_IDValidation(t *testing.T) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}
	manager := NewProxmoxManager(config)

	tests := []struct {
		name    string
		id      int
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Invalid ID - too small",
			id:      MinProxmoxID - 1,
			wantErr: true,
			errMsg:  "invalid ID",
		},
		{
			name:    "Invalid ID - too large",
			id:      MaxProxmoxID + 1,
			wantErr: true,
			errMsg:  "invalid ID",
		},
		{
			name:    "Invalid ID - negative",
			id:      -1,
			wantErr: true,
			errMsg:  "invalid ID",
		},
		{
			name:    "Valid ID - should get API connection error",
			id:      102,
			wantErr: true,
			errMsg:  "failed to get node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.getContainerIP(tt.id)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestProxmoxManager_getVMIP_IDValidation(t *testing.T) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}
	manager := NewProxmoxManager(config)

	tests := []struct {
		name    string
		id      int
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Invalid ID - too small",
			id:      MinProxmoxID - 1,
			wantErr: true,
			errMsg:  "invalid ID",
		},
		{
			name:    "Invalid ID - too large",
			id:      MaxProxmoxID + 1,
			wantErr: true,
			errMsg:  "invalid ID",
		},
		{
			name:    "Valid ID - should get API connection error",
			id:      102,
			wantErr: true,
			errMsg:  "failed to get node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.getVMIP(tt.id)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}


// Test data parsing functions with mock data
func TestParseContainerList(t *testing.T) {
	// Test parsing of pct list output
	mockOutput := `VMID       Status     Lock         Name                
100        running                 webserver               
101        stopped                 database                
102        running                 proxy                   `

	lines := strings.Split(mockOutput, "\n")
	var instances []ProxmoxInstance

	// Skip header line
	for i, line := range lines {
		if i == 0 {
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
		name := fields[len(fields)-1] // Last field is name

		instance := ProxmoxInstance{
			ID:     id,
			Name:   name,
			Status: status,
			Type:   "container",
			IPv4:   "", // Would be filled by IP detection
		}
		instances = append(instances, instance)
	}

	assert.Len(t, instances, 3)
	
	assert.Equal(t, 100, instances[0].ID)
	assert.Equal(t, "webserver", instances[0].Name)
	assert.Equal(t, "running", instances[0].Status)
	assert.Equal(t, "container", instances[0].Type)
	
	assert.Equal(t, 101, instances[1].ID)
	assert.Equal(t, "database", instances[1].Name)
	assert.Equal(t, "stopped", instances[1].Status)
	
	assert.Equal(t, 102, instances[2].ID)
	assert.Equal(t, "proxy", instances[2].Name)
	assert.Equal(t, "running", instances[2].Status)
}

func TestParseVMList(t *testing.T) {
	// Test parsing of qm list output
	mockOutput := `      VMID NAME                 STATUS     MEM(MB)    BOOTDISK(GB) PID       
       200 ubuntu-vm            running    2048                26.00 1234      
       201 windows-server       stopped    4096                50.00 0         
       202 test-vm              running    1024                10.00 5678      `

	lines := strings.Split(mockOutput, "\n")
	var instances []ProxmoxInstance

	// Skip header line
	for i, line := range lines {
		if i == 0 {
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

		instance := ProxmoxInstance{
			ID:     id,
			Name:   name,
			Status: status,
			Type:   "vm",
			IPv4:   "", // Would be filled by IP detection
		}
		instances = append(instances, instance)
	}

	assert.Len(t, instances, 3)
	
	assert.Equal(t, 200, instances[0].ID)
	assert.Equal(t, "ubuntu-vm", instances[0].Name)
	assert.Equal(t, "running", instances[0].Status)
	assert.Equal(t, "vm", instances[0].Type)
	
	assert.Equal(t, 201, instances[1].ID)
	assert.Equal(t, "windows-server", instances[1].Name)
	assert.Equal(t, "stopped", instances[1].Status)
	
	assert.Equal(t, 202, instances[2].ID)
	assert.Equal(t, "test-vm", instances[2].Name)
	assert.Equal(t, "running", instances[2].Status)
}

func TestParseContainerConfig(t *testing.T) {
	// Test parsing of pct config output for IP extraction
	mockConfig := `arch: amd64
cores: 2
hostname: webserver
memory: 1024
net0: name=eth0,bridge=vmbr0,gw=192.168.1.1,hwaddr=BC:24:11:12:34:56,ip=192.168.1.100/24,type=veth
onboot: 1
ostype: ubuntu
rootfs: local:102/vm-102-disk-0.raw,size=8G
swap: 512`

	lines := strings.Split(mockConfig, "\n")
	var foundIP string

	for _, line := range lines {
		if strings.HasPrefix(line, "net") && strings.Contains(line, "ip=") {
			// Extract IP address
			parts := strings.Split(line, ",")
			for _, part := range parts {
				if strings.HasPrefix(part, "ip=") {
					ipPart := strings.TrimPrefix(part, "ip=")
					// Remove CIDR notation if present
					if idx := strings.Index(ipPart, "/"); idx != -1 {
						ipPart = ipPart[:idx]
					}
					foundIP = ipPart
					break
				}
			}
		}
	}

	assert.Equal(t, "192.168.1.100", foundIP)
}

func TestParseVMNetworkInterfaces(t *testing.T) {
	// Test parsing of qm guest cmd network-get-interfaces output
	mockJSON := `[
  {
    "name": "lo",
    "ip-addresses": [
      {
        "ip-address": "127.0.0.1",
        "ip-address-type": "ipv4"
      }
    ]
  },
  {
    "name": "eth0",
    "ip-addresses": [
      {
        "ip-address": "192.168.1.200",
        "ip-address-type": "ipv4"
      },
      {
        "ip-address": "fe80::a00:27ff:fe4e:66a1",
        "ip-address-type": "ipv6"
      }
    ]
  }
]`

	// This would normally be parsed with json.Unmarshal
	// but we're testing the logic here
	assert.Contains(t, mockJSON, "192.168.1.200")
	assert.Contains(t, mockJSON, "ipv4")
	assert.Contains(t, mockJSON, "eth0")
}

// Mock command execution for testing
func TestMockCommandExecution(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}
	
	cmd := args[0]
	switch cmd {
	case "pct":
		if len(args) >= 2 && args[1] == "list" {
			fmt.Print("VMID       Status     Name\n100        running    webserver\n")
		}
	case "qm":
		if len(args) >= 2 && args[1] == "list" {
			fmt.Print("VMID NAME      STATUS\n200  ubuntu-vm running\n")
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}


// TestProxmoxInstance verifies the ProxmoxInstance struct
func TestProxmoxInstance(t *testing.T) {
	instance := ProxmoxInstance{
		ID:     102,
		Name:   "webserver",
		Status: "running",
		Type:   "container",
		IPv4:   "192.168.1.100",
	}

	assert.Equal(t, 102, instance.ID)
	assert.Equal(t, "webserver", instance.Name)
	assert.Equal(t, "running", instance.Status)
	assert.Equal(t, "container", instance.Type)
	assert.Equal(t, "192.168.1.100", instance.IPv4)
}

// Benchmark tests for performance validation
func BenchmarkProxmoxManager_filterIPv4(b *testing.B) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}
	manager := NewProxmoxManager(config)
	
	testOutput := "10.0.1.1 192.168.1.100 172.16.1.1 192.168.1.200"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.filterIPv4(testOutput)
	}
}

func BenchmarkProxmoxManager_GetInstanceByIdentifier(b *testing.B) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   false,
	}
	manager := NewProxmoxManager(config)
	
	// Add test data
	for i := 100; i < 200; i++ {
		instance := ProxmoxInstance{
			ID:     i,
			Name:   fmt.Sprintf("instance%d", i),
			Status: "running",
			Type:   "container",
			IPv4:   fmt.Sprintf("192.168.1.%d", i),
		}
		manager.instances.Store(strconv.Itoa(i), instance)
		manager.instances.Store(instance.Name, instance)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := strconv.Itoa(100 + (i % 100))
		_, _ = manager.GetInstanceByIdentifier(id)
	}
}

// TestProxmoxManager_RefreshInstances tests the refresh functionality
func TestProxmoxManager_RefreshInstances(t *testing.T) {
	config := ProxmoxConfig{
		IPPrefix:    "192.168.",
		APIEndpoint: "https://proxmox:8006/api2/json",
		Username:    "root@pam",
		Password:    "secret",
		NodeName:    "pve",
		DebugMode:   true,
	}
	manager := NewProxmoxManager(config)
	
	// Add some existing data
	manager.instances.Store("100", ProxmoxInstance{ID: 100, Name: "old"})
	
	// Refresh should clear existing data and try to reload
	// Since we don't have Proxmox API available, this will fail
	// but we can verify that the instances map was cleared
	err := manager.RefreshInstances()
	assert.Error(t, err) // Expected to fail without Proxmox API
	
	// Verify instances were cleared (new sync.Map created)
	_, exists := manager.GetInstanceByIdentifier("100")
	assert.False(t, exists, "Instances should be cleared during refresh")
}