// Package dns implements a DNS server for Proxmox VE environments
// that resolves container and VM names to their IP addresses.
//
// The server automatically discovers Proxmox containers and VMs,
// caches their information, and provides DNS A record responses
// for queries matching the configured zone.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"
	"proxmox-dns-server/pkg/config"
	"proxmox-dns-server/pkg/proxmox"
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
	GetInstanceByIdentifier(identifier string) (proxmox.ProxmoxInstance, bool)
}

// Server represents the main DNS server that handles incoming DNS queries
// and resolves them to Proxmox container and VM IP addresses.
type Server struct {
	config  config.ServerConfig      // Server configuration
	proxmox ProxmoxManagerInterface // Manager for Proxmox instance data
	server  *dns.Server             // Underlying DNS server
	ctx     context.Context         // Context for graceful shutdown
	cancel  context.CancelFunc      // Cancel function for context
	wg      sync.WaitGroup          // WaitGroup for managing goroutines
	logger  *zap.SugaredLogger      // Logger for logging messages
}

// NewServer creates a new DNS server instance with the given configuration.
// It initializes the Proxmox manager and sets up the context for graceful shutdown.
func NewServer(parentCtx context.Context, serverConfig config.ServerConfig, proxmoxConfig config.ProxmoxConfig, logger *zap.SugaredLogger) *Server {
	ctx, cancel := context.WithCancel(parentCtx)
	return &Server{
		config:  serverConfig,
		proxmox: proxmox.NewProxmoxManager(proxmoxConfig, logger),
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger,
	}
}

// Start starts the DNS server and begins listening for DNS queries.
// It configures the network binding, initializes instance data,
// and starts the periodic refresh routine. The server runs until
// the context is cancelled or an error occurs.
func (ds *Server) Start() error {
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
		ds.logger.Infow("Starting DNS server",
			"zone", ds.config.Zone,
			"interface", ds.config.BindInterface,
			"address", ip.String(),
			"port", ds.config.Port,
		)
	} else {
		addr = ":" + ds.config.Port
		ds.logger.Infow("Starting DNS server",
			"zone", ds.config.Zone,
			"interface", "all",
			"port", ds.config.Port,
		)
	}

	if err := ds.proxmox.RefreshInstances(); err != nil {
		ds.logger.Warnw("DNS server startup: instance refresh failed", "error", err)
	}

	ds.wg.Add(1)
	go ds.periodicRefresh()

	dns.HandleFunc(".", ds.handleDNSRequest)

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
			ds.logger.Errorw("DNS server context cancellation", "error", shutdownErr)
		}
		return ds.ctx.Err()
	}
}

// Stop gracefully shuts down the DNS server.
// It cancels the context, waits for all goroutines to finish,
// and then stops the DNS server.
func (ds *Server) Stop() error {
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
func (ds *Server) periodicRefresh() {
	defer ds.wg.Done()

	ticker := time.NewTicker(ds.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ds.ctx.Done():
			if ds.config.DebugMode {
				ds.logger.Debug("Stopping periodic refresh...")
			}
			return
		case <-ticker.C:
			if err := ds.proxmox.RefreshInstances(); err != nil {
				ds.logger.Warnw("Periodic refresh failed", "error", err)
			}
		}
	}
}

// handleDNSRequest processes incoming DNS queries.
// If the query is for the configured zone, it resolves A records for Proxmox instances.
// Otherwise, it forwards the query to an upstream DNS server, if configured.
func (ds *Server) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	clientAddr := w.RemoteAddr().String()

	for _, q := range r.Question {
		ds.logger.Debugw("DNS Request",
			"client", clientAddr,
			"type", dns.TypeToString[q.Qtype],
			"name", q.Name,
		)

		// Handle local zone queries
		if q.Qtype == dns.TypeA && strings.HasSuffix(q.Name, ds.config.Zone+".") {
			if answer := ds.resolveA(q.Name); answer != nil {
				m.Answer = append(m.Answer, answer)
			} else {
				ds.logger.Warnw("DNS Request failed: No record found",
					"name", q.Name,
				)
				m.SetRcode(r, dns.RcodeNameError)
			}
		} else if ds.config.Upstream != "" {
			// Forward other queries to the upstream server
			resp, err := ds.forwardQuery(r)
			if err != nil {
				ds.logger.Errorw("Failed to forward query",
					"name", q.Name,
					"error", err,
				)
				m.SetRcode(r, dns.RcodeServerFailure)
			} else {
				m = resp
			}
		} else {
			// No upstream server configured, reject non-local queries
			ds.logger.Warnw("DNS Request failed: Unsupported query type or wrong zone",
				"type", dns.TypeToString[q.Qtype],
				"name", q.Name,
			)
			m.SetRcode(r, dns.RcodeNameError)
		}
	}

	w.WriteMsg(m)
}

// resolveA resolves a DNS A record query for a given name.
// It attempts to match the name to a Proxmox container or VM
// by ID or name, returning the corresponding IP address.
// Returns nil if no matching instance is found or has no IP.
func (ds *Server) resolveA(name string) dns.RR {
	// Pre-compute zone suffix to avoid repeated string concatenation
	zoneSuffix := "." + ds.config.Zone

	// Trim trailing dot once
	name = strings.TrimSuffix(name, ".")

	if !strings.HasSuffix(name, zoneSuffix) {
		ds.logger.Debugw("Name does not match zone",
			"name", name,
			"zone", ds.config.Zone,
		)
		return nil
	}

	// Extract identifier more efficiently
	identifier := name[:len(name)-len(zoneSuffix)]
	ds.logger.Debugw("Looking up identifier",
		"identifier", identifier,
		"name", name,
	)

	instance, exists := ds.proxmox.GetInstanceByIdentifier(identifier)
	if !exists {
		ds.logger.Debugw("No instance found for identifier",
			"identifier", identifier,
		)
		return nil
	}

	if instance.IPv4 == "" {
		ds.logger.Debugw("Instance has no IPv4 address",
			"name", instance.Name,
			"identifier", identifier,
		)
		return nil
	}

	ip := net.ParseIP(instance.IPv4)
	if ip == nil {
		ds.logger.Debugw("Invalid IP address for instance",
			"ip", instance.IPv4,
			"name", instance.Name,
		)
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

	ds.logger.Infow("Successfully resolved",
		"name", name,
		"ip", instance.IPv4,
		"instance", instance.Name,
		"type", instance.Type,
	)
	return rr
}

// forwardQuery sends a DNS query to the configured upstream server and returns the response.
func (ds *Server) forwardQuery(r *dns.Msg) (*dns.Msg, error) {
	c := new(dns.Client)
	resp, _, err := c.Exchange(r, ds.config.Upstream)
	return resp, err
}
