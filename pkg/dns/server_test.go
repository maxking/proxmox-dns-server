package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"proxmox-dns-server/pkg/config"
	"proxmox-dns-server/pkg/proxmox"
)

func TestNewServer(t *testing.T) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		BindInterface:   "",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.NotNil(t, server.proxmox)
	assert.NotNil(t, server.ctx)
	assert.NotNil(t, server.cancel)
}

func TestDNSServerErrorConstants(t *testing.T) {
	assert.NotEmpty(t, ErrInterfaceNotFound.Error())
	assert.NotEmpty(t, ErrNoIPv4Address.Error())
	assert.NotEmpty(t, ErrDNSServerStartup.Error())
	assert.NotEmpty(t, ErrDNSServerShutdown.Error())
	assert.NotEmpty(t, ErrInstanceRefresh.Error())

	assert.Equal(t, "network interface not found", ErrInterfaceNotFound.Error())
	assert.Equal(t, "no IPv4 address found", ErrNoIPv4Address.Error())
	assert.Equal(t, "DNS server startup failed", ErrDNSServerStartup.Error())
	assert.Equal(t, "DNS server shutdown failed", ErrDNSServerShutdown.Error())
	assert.Equal(t, "instance refresh failed", ErrInstanceRefresh.Error())
}

// MockProxmoxManager implements ProxmoxManagerInterface for testing DNS server functionality
type MockProxmoxManager struct {
	instances map[string]proxmox.ProxmoxInstance
	refreshed bool
	mutex     sync.RWMutex
}

func NewMockProxmoxManager() *MockProxmoxManager {
	return &MockProxmoxManager{
		instances: make(map[string]proxmox.ProxmoxInstance),
	}
}

func (m *MockProxmoxManager) RefreshInstances() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.refreshed = true
	return nil
}

func (m *MockProxmoxManager) GetInstanceByIdentifier(identifier string) (proxmox.ProxmoxInstance, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	instance, exists := m.instances[identifier]
	return instance, exists
}

func (m *MockProxmoxManager) AddInstance(identifier string, instance proxmox.ProxmoxInstance) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.instances[identifier] = instance
}

func (m *MockProxmoxManager) WasRefreshed() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.refreshed
}

func TestServer_resolveA(t *testing.T) {
	tests := []struct {
		name            string
		queryName       string
		zone            string
		instances       map[string]proxmox.ProxmoxInstance
		expectedIP      string
		expectedNil     bool
	}{
		{
			name:      "Resolve by ID - container",
			queryName: "102.example.com.",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{
				"102": {
					ID:     102,
					Name:   "webserver",
					Status: "running",
					Type:   "container",
					IPv4:   "192.168.1.100",
				},
			},
			expectedIP:  "192.168.1.100",
			expectedNil: false,
		},
		{
			name:      "Resolve by name - VM",
			queryName: "dbserver.example.com.",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{
				"dbserver": {
					ID:     201,
					Name:   "dbserver",
					Status: "running",
					Type:   "vm",
					IPv4:   "192.168.1.200",
				},
			},
			expectedIP:  "192.168.1.200",
			expectedNil: false,
		},
		{
			name:      "Instance not found",
			queryName: "notfound.example.com.",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{},
			expectedNil: true,
		},
		{
			name:      "Instance with no IP",
			queryName: "noip.example.com.",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{
				"noip": {
					ID:     103,
					Name:   "noip",
					Status: "running",
					Type:   "container",
					IPv4:   "",
				},
			},
			expectedNil: true,
		},
		{
			name:      "Instance with invalid IP",
			queryName: "badip.example.com.",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{
				"badip": {
					ID:     104,
					Name:   "badip",
					Status: "running",
					Type:   "container",
					IPv4:   "invalid-ip",
				},
			},
			expectedNil: true,
		},
		{
			name:      "Wrong zone",
			queryName: "102.wrongzone.com.",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{
				"102": {
					ID:     102,
					Name:   "webserver",
					Status: "running",
					Type:   "container",
					IPv4:   "192.168.1.100",
				},
			},
			expectedNil: true,
		},
		{
			name:      "Query without trailing dot",
			queryName: "105.example.com",
			zone:      "example.com",
			instances: map[string]proxmox.ProxmoxInstance{
				"105": {
					ID:     105,
					Name:   "testserver",
					Status: "running",
					Type:   "container",
					IPv4:   "192.168.1.105",
				},
			},
			expectedIP:  "192.168.1.105",
			expectedNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := config.ServerConfig{
				Zone:            tt.zone,
				Port:            "53",
				IPPrefix:        "192.168.",
				RefreshInterval: 30 * time.Second,
				DebugMode:       true,
			}

			server := NewServer(ctx, cfg)

			// Replace the proxmox manager with our mock
			mockProxmox := NewMockProxmoxManager()
			for id, instance := range tt.instances {
				mockProxmox.AddInstance(id, instance)
			}
			server.proxmox = mockProxmox

			result := server.resolveA(tt.queryName)

			if tt.expectedNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				aRecord, ok := result.(*dns.A)
				assert.True(t, ok)
				assert.Equal(t, tt.expectedIP, aRecord.A.String())

				// Check the DNS record header
				expectedName := tt.queryName
				if expectedName[len(expectedName)-1] != '.' {
					expectedName += "."
				}
				assert.Equal(t, expectedName, aRecord.Hdr.Name)
				assert.Equal(t, uint16(dns.TypeA), aRecord.Hdr.Rrtype)
				assert.Equal(t, uint16(dns.ClassINET), aRecord.Hdr.Class)
				assert.Equal(t, uint32(300), aRecord.Hdr.Ttl)
			}
		})
	}
}

func TestServer_handleDNSRequest(t *testing.T) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	// Setup mock proxmox manager
	mockProxmox := NewMockProxmoxManager()
	mockProxmox.AddInstance("102", proxmox.ProxmoxInstance{
		ID:     102,
		Name:   "webserver",
		Status: "running",
		Type:   "container",
		IPv4:   "192.168.1.100",
	})
	server.proxmox = mockProxmox

	tests := []struct {
		name           string
		queryName      string
		queryType      uint16
		expectedRCode  int
		expectedAnswer bool
		expectedIP     string
	}{
		{
			name:           "Valid A record query",
			queryName:      "102.example.com.",
			queryType:      dns.TypeA,
			expectedRCode:  dns.RcodeSuccess,
			expectedAnswer: true,
			expectedIP:     "192.168.1.100",
		},
		{
			name:           "A record query for non-existent record",
			queryName:      "999.example.com.",
			queryType:      dns.TypeA,
			expectedRCode:  dns.RcodeNameError,
			expectedAnswer: false,
		},
		{
			name:           "AAAA record query (unsupported)",
			queryName:      "102.example.com.",
			queryType:      dns.TypeAAAA,
			expectedRCode:  dns.RcodeNameError,
			expectedAnswer: false,
		},
		{
			name:           "MX record query (unsupported)",
			queryName:      "102.example.com.",
			queryType:      dns.TypeMX,
			expectedRCode:  dns.RcodeNameError,
			expectedAnswer: false,
		},
		{
			name:           "Query for wrong zone",
			queryName:      "102.wrongzone.com.",
			queryType:      dns.TypeA,
			expectedRCode:  dns.RcodeNameError,
			expectedAnswer: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create DNS request
			req := new(dns.Msg)
			req.SetQuestion(tt.queryName, tt.queryType)

			// Mock response writer
			mockWriter := &MockResponseWriter{}

			// Handle the request
			server.handleDNSRequest(mockWriter, req)

			// Verify response
			assert.NotNil(t, mockWriter.response)
			assert.Equal(t, tt.expectedRCode, mockWriter.response.Rcode)

			if tt.expectedAnswer {
				assert.Len(t, mockWriter.response.Answer, 1)
				aRecord, ok := mockWriter.response.Answer[0].(*dns.A)
				assert.True(t, ok)
				assert.Equal(t, tt.expectedIP, aRecord.A.String())
			} else {
				assert.Len(t, mockWriter.response.Answer, 0)
			}

			// Check that response is authoritative
			assert.True(t, mockWriter.response.Authoritative)
		})
	}
}

// TestServer_handleDNSRequest_Forwarding tests the DNS forwarding functionality
func TestServer_handleDNSRequest_Forwarding(t *testing.T) {
	// Start a mock upstream DNS server
	upstreamServer, upstreamAddr, err := startMockUpstreamServer()
	if err != nil {
		t.Fatalf("Failed to start mock upstream server: %v", err)
	}
	defer upstreamServer.Shutdown()

	ctx := context.Background()
	serverConfig := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		Upstream:        upstreamAddr.String(),
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, serverConfig)

	// Create DNS request for an external domain
	req := new(dns.Msg)
	req.SetQuestion("google.com.", dns.TypeA)

	// Mock response writer
	mockWriter := &MockResponseWriter{}

	// Handle the request
	server.handleDNSRequest(mockWriter, req)

	// Verify response
	assert.NotNil(t, mockWriter.response)
	assert.Equal(t, dns.RcodeSuccess, mockWriter.response.Rcode)
	assert.Len(t, mockWriter.response.Answer, 1)
	aRecord, ok := mockWriter.response.Answer[0].(*dns.A)
	assert.True(t, ok)
	assert.Equal(t, "8.8.8.8", aRecord.A.String())
}

// TestServer_handleDNSRequest_Forwarding_UpstreamError tests forwarding when the upstream server fails
func TestServer_handleDNSRequest_Forwarding_UpstreamError(t *testing.T) {
	ctx := context.Background()
	serverConfig := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		Upstream:        "127.0.0.1:9999", // A non-existent server
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, serverConfig)

	// Create DNS request for an external domain
	req := new(dns.Msg)
	req.SetQuestion("google.com.", dns.TypeA)

	// Mock response writer
	mockWriter := &MockResponseWriter{}

	// Handle the request
	server.handleDNSRequest(mockWriter, req)

	// Verify response
	assert.NotNil(t, mockWriter.response)
	assert.Equal(t, dns.RcodeServerFailure, mockWriter.response.Rcode)
}

// startMockUpstreamServer starts a mock DNS server for testing forwarding
func startMockUpstreamServer() (*dns.Server, net.Addr, error) {
	handler := dns.NewServeMux()
	handler.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		if r.Question[0].Name == "google.com." && r.Question[0].Qtype == dns.TypeA {
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "google.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
				A:   net.ParseIP("8.8.8.8"),
			})
		}
		w.WriteMsg(m)
	})

	server := &dns.Server{
		Addr:    "127.0.0.1:0",
		Net:     "udp",
		Handler: handler,
	}

	// Use a channel to signal when the server is ready
	ready := make(chan struct{})
	server.NotifyStartedFunc = func() {
		close(ready)
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			// Don't log error on graceful shutdown
			// if err != dns. {
			// 	log.Printf("Mock upstream server error: %v", err)
			// }
		}
	}()

	// Wait for the server to start
	<-ready

	return server, server.PacketConn.LocalAddr(), nil
}

// MockResponseWriter implements dns.ResponseWriter for testing
type MockResponseWriter struct {
	response   *dns.Msg
	remoteAddr net.Addr
}

func (m *MockResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}
}

func (m *MockResponseWriter) RemoteAddr() net.Addr {
	if m.remoteAddr == nil {
		return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	}
	return m.remoteAddr
}

func (m *MockResponseWriter) WriteMsg(msg *dns.Msg) error {
	m.response = msg
	return nil
}

func (m *MockResponseWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("Write not implemented in mock")
}

func (m *MockResponseWriter) Close() error {
	return nil
}

func (m *MockResponseWriter) TsigStatus() error {
	return nil
}

func (m *MockResponseWriter) TsigTimersOnly(bool) {
}

func (m *MockResponseWriter) Hijack() {
}

func TestServer_Stop(t *testing.T) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	// Test stopping server without starting it
	err := server.Stop()
	assert.NoError(t, err)

	// Verify context is cancelled
	select {
	case <-server.ctx.Done():
		// Context should be cancelled
	default:
		t.Error("Context should be cancelled after Stop()")
	}
}

func TestServer_StartWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately

	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "0", // Use port 0 to avoid binding issues
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	// Starting with cancelled context should return context error
	err := server.Start()
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestServer_StartWithInvalidInterface(t *testing.T) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "0",
		BindInterface:   "nonexistent-interface",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	err := server.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "network interface not found")
}

// TestServer_periodicRefresh tests the periodic refresh functionality
func TestServer_periodicRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		IPPrefix:        "192.168.",
		RefreshInterval: 10 * time.Millisecond, // Very short interval for testing
		DebugMode:       true,
	}

	server := NewServer(ctx, cfg)

	// Replace with mock
	mockProxmox := NewMockProxmoxManager()
	server.proxmox = mockProxmox

	// Start periodic refresh
	server.wg.Add(1)
	go server.periodicRefresh()

	// Wait for at least one refresh cycle
	time.Sleep(20 * time.Millisecond)

	// Cancel context to stop periodic refresh
	cancel()

	// Wait for goroutine to finish
	server.wg.Wait()

	// Verify that refresh was called
	assert.True(t, mockProxmox.WasRefreshed())
}

// TestServer_Integration tests the DNS server with actual network binding
func TestServer_Integration(t *testing.T) {
	// Skip this test in short mode or CI environments
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := config.ServerConfig{
		Zone:            "test.local",
		Port:            "0", // Let the OS choose an available port
		IPPrefix:        "127.0.",
		RefreshInterval: 100 * time.Millisecond,
		DebugMode:       true,
	}

	server := NewServer(ctx, cfg)

	// Replace with mock that has test data
	mockProxmox := NewMockProxmoxManager()
	mockProxmox.AddInstance("100", proxmox.ProxmoxInstance{
		ID:     100,
		Name:   "testcontainer",
		Status: "running",
		Type:   "container",
		IPv4:   "127.0.0.1",
	})
	server.proxmox = mockProxmox

	// Start server in background
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop server
	cancel()

	// Wait for server to stop
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Errorf("Server returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop within timeout")
	}

	// Clean shutdown - may fail if server never started successfully
	err := server.Stop()
	if err != nil && !strings.Contains(err.Error(), "server not started") {
		t.Errorf("Unexpected shutdown error: %v", err)
	}
}

// BenchmarkServer_resolveA benchmarks the DNS resolution function
func BenchmarkServer_resolveA(b *testing.B) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	// Setup mock with test data
	mockProxmox := NewMockProxmoxManager()
	mockProxmox.AddInstance("102", proxmox.ProxmoxInstance{
		ID:     102,
		Name:   "webserver",
		Status: "running",
		Type:   "container",
		IPv4:   "192.168.1.100",
	})
	server.proxmox = mockProxmox

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = server.resolveA("102.example.com.")
	}
}

// BenchmarkServer_handleDNSRequest benchmarks the DNS request handling
func BenchmarkServer_handleDNSRequest(b *testing.B) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		Zone:            "example.com",
		Port:            "53",
		IPPrefix:        "192.168.",
		RefreshInterval: 30 * time.Second,
		DebugMode:       false,
	}

	server := NewServer(ctx, cfg)

	// Setup mock
	mockProxmox := NewMockProxmoxManager()
	mockProxmox.AddInstance("102", proxmox.ProxmoxInstance{
		ID:     102,
		Name:   "webserver",
		Status: "running",
		Type:   "container",
		IPv4:   "192.168.1.100",
	})
	server.proxmox = mockProxmox

	// Create reusable request and writer
	req := new(dns.Msg)
	req.SetQuestion("102.example.com.", dns.TypeA)
	mockWriter := &MockResponseWriter{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.handleDNSRequest(mockWriter, req)
	}
}