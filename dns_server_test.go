package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDNSServerHandlesStaticRecord(t *testing.T) {
	cfg := &Config{
		Zone: "home.local",
		Port: "1053",
		StaticRecords: []StaticRecord{{
			Name:  "nas",
			Type:  "A",
			Value: "192.168.1.10",
			TTL:   180,
		}},
	}

	server := NewDNSServer(cfg)

	msg := new(dns.Msg)
	msg.SetQuestion("nas.home.local.", dns.TypeA)

	writer := newTestResponseWriter()
	server.handleDNSRequest(writer, msg)

	if writer.msg == nil {
		t.Fatalf("expected response message")
	}
	if len(writer.msg.Answer) != 1 {
		t.Fatalf("expected one answer, got %d", len(writer.msg.Answer))
	}
	a, ok := writer.msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("expected A record, got %T", writer.msg.Answer[0])
	}
	if a.A.String() != "192.168.1.10" {
		t.Fatalf("unexpected IP %s", a.A.String())
	}
	if a.Hdr.Name != "nas.home.local." {
		t.Fatalf("unexpected owner name %s", a.Hdr.Name)
	}
	if writer.msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected success rcode, got %d", writer.msg.Rcode)
	}
}

func TestDNSServerFallsBackToProxmox(t *testing.T) {
	cfg := &Config{Zone: "home.local", Port: "1053"}
	server := NewDNSServer(cfg)
	server.static = NewStaticRecordManager() // ensure empty
	server.proxmox.instances["app"] = ProxmoxInstance{
		ID:     101,
		Name:   "app",
		Status: "running",
		Type:   "vm",
		IPv4:   "192.168.1.30",
	}

	msg := new(dns.Msg)
	msg.SetQuestion("app.home.local.", dns.TypeA)

	writer := newTestResponseWriter()
	server.handleDNSRequest(writer, msg)

	if writer.msg == nil {
		t.Fatalf("expected response from proxmox lookup")
	}
	if len(writer.msg.Answer) != 1 {
		t.Fatalf("expected one answer, got %d", len(writer.msg.Answer))
	}
	a, ok := writer.msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("expected A record, got %T", writer.msg.Answer[0])
	}
	if a.A.String() != "192.168.1.30" {
		t.Fatalf("unexpected IP %s", a.A.String())
	}
}

func TestDNSServerForwardsToUpstream(t *testing.T) {
	addr := startAuthoritativeDNSServer(t)

	cfg := &Config{
		Zone:        "home.local",
		Port:        "1053",
		UpstreamDNS: addr,
	}
	server := NewDNSServer(cfg)

	msg := new(dns.Msg)
	msg.SetQuestion("example.org.", dns.TypeA)

	writer := newTestResponseWriter()
	server.handleDNSRequest(writer, msg)

	if writer.msg == nil {
		t.Fatalf("expected upstream response")
	}
	if len(writer.msg.Answer) != 1 {
		t.Fatalf("expected 1 upstream answer, got %d", len(writer.msg.Answer))
	}
	if writer.msg.Answer[0].Header().Name != "example.org." {
		t.Fatalf("unexpected upstream owner %s", writer.msg.Answer[0].Header().Name)
	}
}

func TestDNSServerReturnsNXDomainWhenMissing(t *testing.T) {
	cfg := &Config{Zone: "home.local", Port: "1053"}
	server := NewDNSServer(cfg)

	msg := new(dns.Msg)
	msg.SetQuestion("missing.home.local.", dns.TypeA)

	writer := newTestResponseWriter()
	server.handleDNSRequest(writer, msg)

	if writer.msg == nil {
		t.Fatalf("expected NXDOMAIN response")
	}
	if writer.msg.Rcode != dns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got %d", writer.msg.Rcode)
	}
}

func startAuthoritativeDNSServer(t *testing.T) string {
	t.Helper()

	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		rr, err := dns.NewRR("example.org. 300 IN A 203.0.113.5")
		if err != nil {
			t.Fatalf("failed to build RR: %v", err)
		}
		m.Answer = append(m.Answer, rr)
		if err := w.WriteMsg(m); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	})

	server := &dns.Server{Handler: mux}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	server.PacketConn = pc

	go func() {
		_ = server.ActivateAndServe()
	}()

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.ShutdownContext(ctx)
	})

	return pc.LocalAddr().String()
}
