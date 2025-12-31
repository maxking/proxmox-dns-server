package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type Config struct {
	Zone           string
	Port           string
	Interface      string
	WebPort        string
	WebInterface   string
	IPPrefix       string
	APIURL         string
	APITokenID     string
	APITokenSecret string
	APIInsecure    bool
}

const usageHelp = `Error: %v

Usage:
  %s -zone <zone> -api-url <url> -api-token-id <id> [-port <port>] [-interface <interface>] [-web-port <port>] [-web-interface <interface>] [-ip-prefix <prefix>]

Example:
  %s -zone p01.araj.me -api-url https://proxmox:8006 -api-token-id root@pam!dns -api-token-secret <secret>
  %s -zone p01.araj.me -port 5353 -web-port 8080 -api-url https://proxmox:8006 -api-token-id root@pam!dns -api-token-secret <secret>

This will resolve:
  102.p01.araj.me -> IP of container/VM with ID 102
  mycontainer.p01.araj.me -> IP of container/VM named 'mycontainer'
`

func main() {
	var zone = flag.String("zone", "", "DNS zone to serve (required)")
	var port = flag.String("port", "53", "Port to listen on")
	var iface = flag.String("interface", "", "Interface to bind to (default: all interfaces)")
	var webPort = flag.String("web-port", "8080", "Web UI port to listen on (set to 0 to disable)")
	var webIface = flag.String("web-interface", "", "Interface to bind web UI to (default: all interfaces)")
	var ipPrefix = flag.String("ip-prefix", "192.168.", "IP prefix filter for container/VM IPs")
	var apiURL = flag.String("api-url", "", "Proxmox API base URL (e.g. https://proxmox:8006) (required)")
	var apiTokenID = flag.String("api-token-id", "", "Proxmox API token ID (user@realm!token)")
	var apiTokenSecret = flag.String("api-token-secret", "", "Proxmox API token secret")
	var apiInsecure = flag.Bool("api-insecure", false, "Skip TLS verification for the Proxmox API")

	flag.Parse()

	if *zone == "" {
		err := fmt.Errorf("zone is required")
		fmt.Fprintf(os.Stderr, usageHelp, err, os.Args[0], os.Args[0], os.Args[0])
		os.Exit(1)
	}
	if *apiURL == "" {
		err := fmt.Errorf("api-url is required")
		fmt.Fprintf(os.Stderr, usageHelp, err, os.Args[0], os.Args[0], os.Args[0])
		os.Exit(1)
	}

	config := &Config{
		Zone:           *zone,
		Port:           *port,
		Interface:      *iface,
		WebPort:        *webPort,
		WebInterface:   *webIface,
		IPPrefix:       *ipPrefix,
		APIURL:         *apiURL,
		APITokenID:     *apiTokenID,
		APITokenSecret: *apiTokenSecret,
		APIInsecure:    *apiInsecure,
	}

	if config.APITokenSecret == "" {
		config.APITokenSecret = os.Getenv("PVE_API_TOKEN_SECRET")
	}

	if config.APITokenID == "" || config.APITokenSecret == "" {
		fmt.Fprintln(os.Stderr, "Error: -api-token-id and -api-token-secret are required")
		os.Exit(1)
	}

	client, err := NewProxmoxAPIClient(config.APIURL, config.APITokenID, config.APITokenSecret, config.APIInsecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize API client: %v\n", err)
		os.Exit(1)
	}

	proxmoxManager := NewProxmoxManager(config.IPPrefix, client)
	server := NewDNSServer(config.Zone, config.Port, config.Interface, proxmoxManager)

	var webServer *WebServer
	if config.WebPort != "" && config.WebPort != "0" {
		webServer, err = NewWebServer(config.Zone, config.WebPort, config.WebInterface, proxmoxManager)
		if err != nil {
			log.Fatalf("Failed to create web server: %v", err)
		}

		go func() {
			if err := webServer.Start(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Failed to start web server: %v", err)
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down DNS server...")
		if webServer != nil {
			log.Println("Shutting down web server...")
			if err := webServer.Stop(); err != nil {
				log.Printf("Error stopping web server: %v", err)
			}
		}
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
