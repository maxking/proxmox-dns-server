package main

import "testing"

func TestProxmoxManagerFilterIPv4(t *testing.T) {
	pm := NewProxmoxManager()
	ip, err := pm.filterIPv4("10.0.0.1 192.168.1.99 fe80::1")
	if err != nil {
		t.Fatalf("expected IPv4 match, got error %v", err)
	}
	if ip != "192.168.1.99" {
		t.Fatalf("expected 192.168.1.99, got %s", ip)
	}

	if _, err := pm.filterIPv4("10.0.0.1"); err == nil {
		t.Fatalf("expected error when no suitable IPv4 present")
	}
}

func TestProxmoxManagerGetInstanceByIdentifier(t *testing.T) {
	pm := NewProxmoxManager()
	instance := ProxmoxInstance{ID: 101, Name: "app", IPv4: "192.168.1.30"}
	pm.instances["app"] = instance

	got, ok := pm.GetInstanceByIdentifier("app")
	if !ok {
		t.Fatalf("expected to find instance")
	}
	if got.IPv4 != "192.168.1.30" {
		t.Fatalf("unexpected IPv4 %s", got.IPv4)
	}
}
