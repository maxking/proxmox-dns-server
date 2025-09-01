package main

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestErrorConstants ensures our error constants are defined correctly
func TestErrorConstants(t *testing.T) {
	assert.NotEmpty(t, ErrInvalidConfiguration.Error())
	assert.NotEmpty(t, ErrServerStartup.Error())
	assert.NotEmpty(t, ErrServerShutdown.Error())
	
	assert.Equal(t, "invalid configuration", ErrInvalidConfiguration.Error())
	assert.Equal(t, "server startup failed", ErrServerStartup.Error())
	assert.Equal(t, "server shutdown failed", ErrServerShutdown.Error())
}

// TestUsageHelp verifies the usage help message is properly formatted
func TestUsageHelp(t *testing.T) {
	assert.Contains(t, usageHelp, "Usage:")
	assert.Contains(t, usageHelp, "Examples:")
	assert.Contains(t, usageHelp, "-zone")
	assert.Contains(t, usageHelp, "-port")
	assert.Contains(t, usageHelp, "-interface")
	assert.Contains(t, usageHelp, "-ip-prefix")
	assert.Contains(t, usageHelp, "-debug")
	assert.Contains(t, usageHelp, "-api-endpoint")
	assert.Contains(t, usageHelp, "-username")
	assert.Contains(t, usageHelp, "-password")
	assert.Contains(t, usageHelp, "-api-token")
	assert.Contains(t, usageHelp, "-api-secret")
	assert.Contains(t, usageHelp, "-node")
	assert.Contains(t, usageHelp, "-insecure-tls")
}

// TestMainFlagParsing tests the command line flag parsing behavior
func TestMainFlagParsing(t *testing.T) {
	// Save original command line args
	originalArgs := os.Args
	originalCommandLine := flag.CommandLine
	
	// Reset flag package state
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	
	defer func() {
		// Restore original state
		os.Args = originalArgs
		flag.CommandLine = originalCommandLine
	}()

	tests := []struct {
		name     string
		args     []string
		testFunc func(t *testing.T)
	}{
		{
			name: "All flags provided",
			args: []string{"program", "-zone", "example.com", "-port", "5353", "-interface", "eth0", "-ip-prefix", "10.0.", "-debug", "-api-endpoint", "https://proxmox:8006/api2/json", "-username", "root@pam", "-password", "secret", "-node", "pve"},
			testFunc: func(t *testing.T) {
				os.Args = []string{"program", "-zone", "example.com", "-port", "5353", "-interface", "eth0", "-ip-prefix", "10.0.", "-debug", "-api-endpoint", "https://proxmox:8006/api2/json", "-username", "root@pam", "-password", "secret", "-node", "pve"}
				
				var zone = flag.String("zone", "", "DNS zone to serve (required)")
				var port = flag.String("port", "53", "Port to listen on")
				var iface = flag.String("interface", "", "Interface to bind to (default: all interfaces)")
				var ipPrefix = flag.String("ip-prefix", "192.168.", "IP prefix filter for container/VM IPs")
				var debug = flag.Bool("debug", false, "Enable debug logging")
				var apiEndpoint = flag.String("api-endpoint", "", "Proxmox API endpoint")
				var username = flag.String("username", "", "Proxmox username")
				var password = flag.String("password", "", "Proxmox password")
				var nodeName = flag.String("node", "", "Proxmox node name")
				
				flag.Parse()
				
				assert.Equal(t, "example.com", *zone)
				assert.Equal(t, "5353", *port)
				assert.Equal(t, "eth0", *iface)
				assert.Equal(t, "10.0.", *ipPrefix)
				assert.True(t, *debug)
				assert.Equal(t, "https://proxmox:8006/api2/json", *apiEndpoint)
				assert.Equal(t, "root@pam", *username)
				assert.Equal(t, "secret", *password)
				assert.Equal(t, "pve", *nodeName)
			},
		},
		{
			name: "Minimal flags",
			args: []string{"program", "-zone", "test.local"},
			testFunc: func(t *testing.T) {
				// Reset flag package state for this test
				flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
				
				os.Args = []string{"program", "-zone", "test.local"}
				
				var zone = flag.String("zone", "", "DNS zone to serve (required)")
				var port = flag.String("port", "53", "Port to listen on")
				var iface = flag.String("interface", "", "Interface to bind to (default: all interfaces)")
				var ipPrefix = flag.String("ip-prefix", "192.168.", "IP prefix filter for container/VM IPs")
				var debug = flag.Bool("debug", false, "Enable debug logging")
				
				flag.Parse()
				
				assert.Equal(t, "test.local", *zone)
				assert.Equal(t, "53", *port)      // default value
				assert.Equal(t, "", *iface)       // default value
				assert.Equal(t, "192.168.", *ipPrefix) // default value
				assert.False(t, *debug)           // default value
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flag package state for each test
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			tt.testFunc(t)
		})
	}
}
