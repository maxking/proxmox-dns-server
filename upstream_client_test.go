package main

import (
	"testing"

	"github.com/miekg/dns"
)

func TestUpstreamClientQuery(t *testing.T) {
	addr := startAuthoritativeDNSServer(t)

	client := NewUpstreamClient(addr)
	if client == nil {
		t.Fatalf("expected client instance")
	}

	msg := new(dns.Msg)
	msg.SetQuestion("example.org.", dns.TypeA)

	resp := client.Query(msg)
	if resp == nil {
		t.Fatalf("expected response from upstream server")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	if resp.Answer[0].Header().Name != "example.org." {
		t.Fatalf("unexpected answer name %s", resp.Answer[0].Header().Name)
	}
}

func TestUpstreamClientIsConfigured(t *testing.T) {
	if client := NewUpstreamClient(""); client != nil {
		t.Fatalf("expected nil client when no server configured")
	}

	client := NewUpstreamClient("127.0.0.1:53")
	if client == nil {
		t.Fatalf("expected client when server provided")
	}
	if !client.IsConfigured() {
		t.Fatalf("expected IsConfigured to return true")
	}
}
