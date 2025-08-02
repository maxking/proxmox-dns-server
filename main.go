package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nUsage:\n")
		fmt.Fprintf(os.Stderr, "  %s -zone <zone> [-port <port>] [-config <config-file>]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  %s -zone p01.araj.me\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -zone p01.araj.me -port 5353\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nThis will resolve:\n")
		fmt.Fprintf(os.Stderr, "  102.p01.araj.me -> IP of container/VM with ID 102\n")
		fmt.Fprintf(os.Stderr, "  mycontainer.p01.araj.me -> IP of container/VM named 'mycontainer'\n")
		os.Exit(1)
	}
	
	server := NewDNSServer(config.Zone, config.Port)
	
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