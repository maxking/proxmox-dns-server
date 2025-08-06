package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	Zone      string
	Port      string
	Interface string
	IPPrefix  string
}

const usageHelp = `Error: %v

Usage:
  %s -zone <zone> [-port <port>] [-interface <interface>] [-ip-prefix <prefix>]

Example:
  %s -zone p01.araj.me
  %s -zone p01.araj.me -port 5353 -ip-prefix 10.0.

This will resolve:
  102.p01.araj.me -> IP of container/VM with ID 102
  mycontainer.p01.araj.me -> IP of container/VM named 'mycontainer'
`

func main() {
	var zone = flag.String("zone", "", "DNS zone to serve (required)")
	var port = flag.String("port", "53", "Port to listen on")
	var iface = flag.String("interface", "", "Interface to bind to (default: all interfaces)")
	var ipPrefix = flag.String("ip-prefix", "192.168.", "IP prefix filter for container/VM IPs")
	
	flag.Parse()
	
	if *zone == "" {
		fmt.Fprintf(os.Stderr, usageHelp, "zone is required", os.Args[0], os.Args[0], os.Args[0])
		os.Exit(1)
	}
	
	config := &Config{
		Zone:      *zone,
		Port:      *port,
		Interface: *iface,
		IPPrefix:  *ipPrefix,
	}
	
	server := NewDNSServer(config.Zone, config.Port, config.Interface, config.IPPrefix)
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
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
		log.Printf("Starting Proxmox DNS server for zone %s on port %s", config.Zone, config.Port)
		if err := server.Start(); err != nil {
			log.Printf("DNS server error: %v", err)
			cancel()
		}
	}()
	
	<-ctx.Done()
	log.Println("Shutting down DNS server...")
	
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Stop(); err != nil {
			log.Printf("Error stopping server: %v", err)
		}
		wg.Wait()
	}()
	
	select {
	case <-done:
		log.Println("Server stopped gracefully")
	case <-shutdownCtx.Done():
		log.Println("Shutdown timeout exceeded, forcing exit")
	}
}