package main

import (
	"path/filepath"
	"testing"

	"github.com/miekg/dns"
)

func TestStaticRecordManagerResolveWithShortAndFQDN(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "records.json")
	writeJSON(t, filePath, map[string]any{
		"records": []StaticRecord{
			{Name: "printer", Type: "A", Value: "192.168.1.20", TTL: 450},
		},
	})

	manager := NewStaticRecordManager()
	cfg := &Config{
		Zone: "home.local",
		StaticRecords: []StaticRecord{
			{Name: "nas", Type: "A", Value: "192.168.1.10", TTL: 120},
		},
		RecordsFile: filePath,
	}

	if err := manager.LoadRecords(cfg); err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	tests := []struct {
		identifier string
		queryName  string
		value      string
		ttl        uint32
	}{
		{identifier: "nas", queryName: "nas.home.local", value: "192.168.1.10", ttl: 120},
		{identifier: "nas.home.local", queryName: "nas.home.local", value: "192.168.1.10", ttl: 120},
		{identifier: "printer", queryName: "printer.home.local", value: "192.168.1.20", ttl: 450},
	}

	for _, tc := range tests {
		answers := manager.ResolveRecord(tc.identifier, tc.queryName, "A")
		if len(answers) != 1 {
			t.Fatalf("expected 1 answer for %s/%s got %d", tc.identifier, tc.queryName, len(answers))
		}
		a, ok := answers[0].(*dns.A)
		if !ok {
			t.Fatalf("expected *dns.A record, got %T", answers[0])
		}
		if a.A.String() != tc.value {
			t.Fatalf("expected IP %s, got %s", tc.value, a.A.String())
		}
		if a.Hdr.Name != dns.Fqdn(tc.queryName) {
			t.Fatalf("unexpected owner name %s", a.Hdr.Name)
		}
		if a.Hdr.Ttl != tc.ttl {
			t.Fatalf("expected TTL %d, got %d", tc.ttl, a.Hdr.Ttl)
		}
	}
}

func TestStaticRecordManagerHasRecordUsesNormalization(t *testing.T) {
	manager := NewStaticRecordManager()
	cfg := &Config{
		Zone: "home.local",
		StaticRecords: []StaticRecord{
			{Name: "gateway", Type: "A", Value: "192.168.1.1", TTL: 300},
		},
	}
	if err := manager.LoadRecords(cfg); err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	if !manager.HasRecord("gateway") {
		t.Fatalf("expected HasRecord to match short name")
	}
	if !manager.HasRecord("gateway.home.local.") {
		t.Fatalf("expected HasRecord to match fully qualified name")
	}
	if manager.HasRecord("missing") {
		t.Fatalf("expected missing record to return false")
	}
}
