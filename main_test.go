package main

import (
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
	assert.Contains(t, usageHelp, "generate-config")
	assert.Contains(t, usageHelp, "-config")
	assert.Contains(t, usageHelp, "/etc/proxmox-dns-server/config.json")
}
