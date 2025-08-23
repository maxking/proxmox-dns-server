// Package main implements a DNS server for Proxmox VE environments
// that resolves container and VM names to their IP addresses.
//
// The server automatically discovers Proxmox containers and VMs,
// caches their information, and provides DNS A record responses
// for queries matching the configured zone.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Application-level error variables
var (
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrServerStartup        = errors.New("server startup failed")
	ErrServerShutdown       = errors.New("server shutdown failed")
)

// Config is deprecated, keeping for reference but will be removed.
// Use ServerConfig from dns_server.go instead.
type Config struct {
	Zone      string // DNS zone to serve
	Port      string // Port to listen on
	Interface string // Network interface to bind to
	IPPrefix  string // IP prefix filter for container/VM IPs
}

const usageHelp = `Error: %v

Usage:
  %s -zone <zone> [-port <port>] [-interface <interface>] [-ip-prefix <prefix>] [-debug]

Example:
  %s -zone p01.araj.me
  %s -zone p01.araj.me -port 5353 -ip-prefix 10.0. -debug

This will resolve:
  102.p01.araj.me -> IP of container/VM with ID 102
  mycontainer.p01.araj.me -> IP of container/VM named 'mycontainer'
`

// main is the entry point for the Proxmox DNS server application.
// It parses command line arguments, configures the DNS server,
// and handles graceful shutdown when receiving signals.
func main() {
	var zone = flag.String("zone", "", "DNS zone to serve (required)")
	var port = flag.String("port", "53", "Port to listen on")
	var iface = flag.String("interface", "", "Interface to bind to (default: all interfaces)")
	var ipPrefix = flag.String("ip-prefix", "192.168.", "IP prefix filter for container/VM IPs")
	var debug = flag.Bool("debug", false, "Enable debug logging")

	flag.Parse()

	if *zone == "" {
		fmt.Fprintf(os.Stderr, usageHelp, fmt.Sprintf("%v: zone is required", ErrInvalidConfiguration), os.Args[0], os.Args[0], os.Args[0])
		os.Exit(1)
	}

	// Build server configuration
	serverConfig := NewServerConfigFromFlags(*zone, *port, *iface, *ipPrefix)
	serverConfig.DebugMode = *debug

	// Validate server configuration
	if err := serverConfig.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation failed: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewDNSServer(ctx, *serverConfig)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-sigChan
		log.Println("Received shutdown signal, stopping server...")
		cancel()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Starting Proxmox DNS server for zone %s on port %s", serverConfig.Zone, serverConfig.Port)
		if err := server.Start(); err != nil {
			log.Printf("DNS server startup failed: %v", fmt.Errorf("%w for zone %s on port %s: %v", ErrServerStartup, serverConfig.Zone, serverConfig.Port, err))
			cancel()
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down DNS server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Wait for all goroutines to finish first
	wg.Wait()

	// Then stop the server
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Stop(); err != nil {
			log.Printf("Server shutdown error: %v", fmt.Errorf("%w: %v", ErrServerShutdown, err))
		}
	}()

	select {
	case <-done:
		log.Println("Server stopped gracefully")
	case <-shutdownCtx.Done():
		log.Println("Shutdown timeout exceeded, forcing exit")
	}
}
