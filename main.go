package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const usageHelp = `Error: %v

Usage:
  %s -zone <zone> [-port <port>] [-config <config-file>]

Example:
  %s -zone p01.araj.me
  %s -zone p01.araj.me -port 5353

This will resolve:
  102.p01.araj.me -> IP of container/VM with ID 102
  mycontainer.p01.araj.me -> IP of container/VM named 'mycontainer'
`

func main() {
	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, usageHelp, err, os.Args[0], os.Args[0], os.Args[0])
		os.Exit(1)
	}
	
	server := NewDNSServer(config.Zone, config.Port, config.Interface)
	
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		<-sigChan
		log.Println("Shutting down DNS server...")
		if err := server.Stop(); err != nil {
			log.Printf("Error stopping server: %v", err)
		}
		os.Exit(0)
	}()
	
	log.Printf("Starting Proxmox DNS server for zone %s on port %s", config.Zone, config.Port)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start DNS server: %v", err)
	}
}