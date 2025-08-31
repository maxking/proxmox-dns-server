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
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"proxmox-dns-server/pkg/config"
)

// DNS server specific error variables
var (
	ErrInterfaceNotFound = errors.New("network interface not found")
	ErrNoIPv4Address     = errors.New("no IPv4 address found")
	ErrDNSServerStartup  = errors.New("DNS server startup failed")
	ErrDNSServerShutdown = errors.New("DNS server shutdown failed")
	ErrInstanceRefresh   = errors.New("instance refresh failed")
)

// ProxmoxManagerInterface defines the interface for Proxmox instance management
type ProxmoxManagerInterface interface {
	RefreshInstances() error
	GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool)
}

// DNSServer represents the main DNS server that handles incoming DNS queries
// and resolves them to Proxmox container and VM IP addresses.
type DNSServer struct {
	config  config.ServerConfig      // Server configuration
	proxmox ProxmoxManagerInterface // Manager for Proxmox instance data
	server  *dns.Server              // Underlying DNS server
	ctx     context.Context          // Context for graceful shutdown
	cancel  context.CancelFunc       // Cancel function for context
	wg      sync.WaitGroup           // WaitGroup for managing goroutines
}

// NewDNSServer creates a new DNS server instance with the given configuration.
// It initializes the Proxmox manager and sets up the context for graceful shutdown.
func NewDNSServer(parentCtx context.Context, config config.ServerConfig, proxmoxConfig config.ProxmoxConfig) *DNSServer {
	ctx, cancel := context.WithCancel(parentCtx)
	return &DNSServer{
		config:  config,
		proxmox: NewProxmoxManager(proxmoxConfig),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the DNS server and begins listening for DNS queries.
// It configures the network binding, initializes instance data,
// and starts the periodic refresh routine. The server runs until
// the context is cancelled or an error occurs.
func (ds *DNSServer) Start() error {
	// Check if context is already cancelled
	select {
	case <-ds.ctx.Done():
		return ds.ctx.Err()
	default:
	}

	var addr string
	if ds.config.BindInterface != "" {
		// Get IP address of the specified interface
		iface, err := net.InterfaceByName(ds.config.BindInterface)
		if err != nil {
			return fmt.Errorf("DNS server interface lookup: %w '%s': %v", ErrInterfaceNotFound, ds.config.BindInterface, err)
		}

		addrs, err := iface.Addrs()
		if err != nil {
			return fmt.Errorf("DNS server interface addresses: failed to get addresses for interface '%s': %w", ds.config.BindInterface, err)
		}

		var ip net.IP
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ipNet.IP.To4() != nil {
					ip = ipNet.IP
					break
				}
			}
		}

		if ip == nil {
			return fmt.Errorf("DNS server interface binding: %w on interface '%s'", ErrNoIPv4Address, ds.config.BindInterface)
		}

		addr = ip.String() + ":" + ds.config.Port
		log.Printf("Starting DNS server for zone %s on interface %s (%s:%s)", ds.config.Zone, ds.config.BindInterface, ip.String(), ds.config.Port)
	} else {
		addr = ":" + ds.config.Port
		log.Printf("Starting DNS server for zone %s on all interfaces (port %s)", ds.config.Zone, ds.config.Port)
	}

	if err := ds.proxmox.RefreshInstances(); err != nil {
		log.Printf("Warning: DNS server startup: %v: %v", ErrInstanceRefresh, err)
	}

	ds.wg.Add(1)
	go ds.periodicRefresh()

	dns.HandleFunc(ds.config.Zone, ds.handleDNSRequest)

	ds.server = &dns.Server{
		Addr: addr,
		Net:  "udp",
	}

	// Start server in a goroutine and monitor context
	errorCh := make(chan error, 1)
	go func() {
		errorCh <- ds.server.ListenAndServe()
	}()

	// Wait for either server error or context cancellation
	select {
	case err := <-errorCh:
		return err
	case <-ds.ctx.Done():
		// Context was cancelled, shutdown server
		if shutdownErr := ds.server.Shutdown(); shutdownErr != nil {
			log.Printf("DNS server context cancellation: %v: %v", ErrDNSServerShutdown, shutdownErr)
		}
		return ds.ctx.Err()
	}
}

// Stop gracefully shuts down the DNS server.
// It cancels the context, waits for all goroutines to finish,
// and then stops the DNS server.
func (ds *DNSServer) Stop() error {
	ds.cancel()
	ds.wg.Wait()

	if ds.server != nil {
		if err := ds.server.Shutdown(); err != nil {
			return fmt.Errorf("DNS server stop: %w: %v", ErrDNSServerShutdown, err)
		}
	}
	return nil
}

// periodicRefresh runs in a separate goroutine and periodically refreshes
// the Proxmox instance data at the configured interval.
func (ds *DNSServer) periodicRefresh() {
	defer ds.wg.Done()

	ticker := time.NewTicker(ds.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ds.ctx.Done():
			if ds.config.DebugMode {
				log.Println("Stopping periodic refresh...")
			}
			return
		case <-ticker.C:
			if err := ds.proxmox.RefreshInstances(); err != nil {
				log.Printf("Periodic refresh: %v: %v", ErrInstanceRefresh, err)
			}
		}
	}
}

// handleDNSRequest processes incoming DNS queries and responds with
// A records for matching Proxmox containers and VMs.
// It only handles A record queries for the configured DNS zone.
func (ds *DNSServer) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	clientAddr := w.RemoteAddr().String()

	for _, q := range r.Question {
		log.Printf("DNS Request from %s: %s %s", clientAddr, dns.TypeToString[q.Qtype], q.Name)

		if q.Qtype == dns.TypeA && strings.HasSuffix(q.Name, ds.config.Zone+".") {
			if answer := ds.resolveA(q.Name); answer != nil {
				m.Answer = append(m.Answer, answer)
			} else {
				log.Printf("DNS Request failed: No record found for %s", q.Name)
				m.SetRcode(r, dns.RcodeNameError)
			}
		} else {
			log.Printf("DNS Request failed: Unsupported query type %s or wrong zone for %s", dns.TypeToString[q.Qtype], q.Name)
			m.SetRcode(r, dns.RcodeNameError)
		}
	}

	w.WriteMsg(m)
}

// resolveA resolves a DNS A record query for a given name.
// It attempts to match the name to a Proxmox container or VM
// by ID or name, returning the corresponding IP address.
// Returns nil if no matching instance is found or has no IP.
func (ds *DNSServer) resolveA(name string) dns.RR {
	// Pre-compute zone suffix to avoid repeated string concatenation
	zoneSuffix := "." + ds.config.Zone

	// Trim trailing dot once
	name = strings.TrimSuffix(name, ".")

	if !strings.HasSuffix(name, zoneSuffix) {
		if ds.config.DebugMode {
			log.Printf("Debug: %s does not match zone %s", name, ds.config.Zone)
		}
		return nil
	}

	// Extract identifier more efficiently
	identifier := name[:len(name)-len(zoneSuffix)]
	if ds.config.DebugMode {
		log.Printf("Debug: Looking up identifier '%s' for %s", identifier, name)
	}

	instance, exists := ds.proxmox.GetInstanceByIdentifier(identifier)
	if !exists {
		if ds.config.DebugMode {
			log.Printf("Debug: No instance found for identifier '%s'", identifier)
		}
		return nil
	}

	if instance.IPv4 == "" {
		if ds.config.DebugMode {
			log.Printf("Debug: Instance %s (%s) has no IPv4 address", instance.Name, identifier)
		}
		return nil
	}

	ip := net.ParseIP(instance.IPv4)
	if ip == nil {
		if ds.config.DebugMode {
			log.Printf("Debug: Invalid IP address '%s' for instance %s", instance.IPv4, instance.Name)
		}
		return nil
	}

	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   name + ".",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: ip,
	}

	log.Printf("Successfully resolved %s to %s (instance: %s, type: %s)", name, instance.IPv4, instance.Name, instance.Type)
	return rr
}
