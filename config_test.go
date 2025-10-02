package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()

	recordsPath := filepath.Join(dir, "records.json")
	recordsContent := map[string]any{
		"records": []StaticRecord{
			{Name: "printer", Type: "A", Value: "192.168.1.50", TTL: 600},
		},
	}
	writeJSON(t, recordsPath, recordsContent)

	configPath := filepath.Join(dir, "config.json")
	cfg := Config{
		Zone:        "lab.local",
		Port:        "8053",
		UpstreamDNS: "127.0.0.1:8600",
		StaticRecords: []StaticRecord{
			{Name: "nas", Type: "A", Value: "192.168.1.10", TTL: 300},
		},
		RecordsFile: recordsPath,
	}
	writeJSON(t, configPath, cfg)

	oldFlags := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{os.Args[0], "-config", configPath}
	t.Cleanup(func() {
		flag.CommandLine = oldFlags
		os.Args = oldArgs
	})

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if loaded.Zone != "lab.local" {
		t.Fatalf("expected zone lab.local, got %s", loaded.Zone)
	}
	if loaded.Port != "8053" {
		t.Fatalf("expected port 8053, got %s", loaded.Port)
	}
	if loaded.UpstreamDNS != "127.0.0.1:8600" {
		t.Fatalf("unexpected upstream DNS: %s", loaded.UpstreamDNS)
	}
	if len(loaded.StaticRecords) != 1 {
		t.Fatalf("expected 1 inline record, got %d", len(loaded.StaticRecords))
	}
	if loaded.RecordsFile != recordsPath {
		t.Fatalf("expected records file %s, got %s", recordsPath, loaded.RecordsFile)
	}
}

func TestLoadConfigRequiresZone(t *testing.T) {
	oldFlags := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{os.Args[0]}
	t.Cleanup(func() {
		flag.CommandLine = oldFlags
		os.Args = oldArgs
	})

	_, err := LoadConfig()
	if err == nil {
		t.Fatalf("expected error when zone is missing")
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
