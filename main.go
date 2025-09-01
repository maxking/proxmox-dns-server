// Package main implements a DNS server for Proxmox VE environments
// that resolves container and VM names to their IP addresses.
//
// The server automatically discovers Proxmox containers and VMs,
// caches their information, and provides DNS A record responses
// for queries matching the configured zone.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"proxmox-dns-server/pkg/config"
	"proxmox-dns-server/pkg/dns"
)

var (
	logger *zap.SugaredLogger
)

// Application-level error variables
var (
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrServerStartup        = errors.New("server startup failed")
	ErrServerShutdown       = errors.New("server shutdown failed")
)

const usageHelp = `Error: %v

Usage:
  %s [command] [config-file-path]

Commands:
  generate-config   Generate a sample JSON configuration file and print it to stdout

Arguments:
  config-file-path  Path to a JSON configuration file (default: /etc/proxmox-dns-server/config.json)

Examples:
  # Using default configuration file:
  %s
  
  # Using a custom configuration file:
  %s /path/to/config.json

  # Generate a sample configuration:
  %s generate-config
`

// main is the entry point for the Proxmox DNS server application.
// It parses command line arguments, configures the DNS server,
// and handles graceful shutdown when receiving signals.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "generate-config" {
		generateSampleConfig()
		os.Exit(0)
	}

	// Determine config file path
	var configFile string
	if len(os.Args) > 1 && os.Args[1] != "generate-config" {
		configFile = os.Args[1]
	} else {
		configFile = "/etc/proxmox-dns-server/config.json"
	}

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config file '%s': %v\n", configFile, err)
		os.Exit(1)
	}

	// Initialize logger
	logConfig := zap.NewProductionConfig()
	if cfg.Server.DebugMode {
		logConfig.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	}
	baseLogger, err := logConfig.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	logger = baseLogger.Sugar()
	defer logger.Sync()

	// Validate server configuration
	if err := cfg.Server.Validate(); err != nil {
		logger.Fatalw("Server configuration validation failed", "error", err)
	}

	// Validate Proxmox configuration
	if err := cfg.Proxmox.Validate(); err != nil {
		logger.Fatalw("Proxmox configuration validation failed", "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := dns.NewServer(ctx, cfg.Server, cfg.Proxmox, logger)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-sigChan
		logger.Info("Received shutdown signal, stopping server...")
		cancel()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Infow("Starting Proxmox DNS server",
			"zone", cfg.Server.Zone,
			"port", cfg.Server.Port,
		)
		if err := server.Start(); err != nil {
			logger.Errorw("DNS server startup failed",
				"zone", cfg.Server.Zone,
				"port", cfg.Server.Port,
				"error", err,
			)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down DNS server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Wait for all goroutines to finish first
	wg.Wait()

	// Then stop the server
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Stop(); err != nil {
			logger.Errorw("Server shutdown error", "error", err)
		}
	}()

	select {
	case <-done:
		logger.Info("Server stopped gracefully")
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout exceeded, forcing exit")
	}
}

func generateSampleConfig() {
	sampleConfig := config.Config{
		Server: config.ServerConfig{
			Zone:            "example.com",
			Port:            "53",
			BindInterface:   "",
			IPPrefix:        "192.168.1.",
			RefreshInterval: 30 * time.Second,
			DebugMode:       false,
		},
		Proxmox: config.ProxmoxConfig{
			APIEndpoint:    "https://proxmox:8006/api2/json",
			Username:       "root@pam",
			Password:       "your-password",
			APIToken:       "",
			APISecret:      "",
			NodeName:       "",
			InsecureTLS:    false,
			IPPrefix:       "192.168.1.",
			CommandTimeout: 30 * time.Second,
			DebugMode:      false,
		},
	}

	output, err := json.MarshalIndent(sampleConfig, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate sample config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}
