package main

import (
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type DNSServer struct {
	zone     string
	proxmox  *ProxmoxManager
	server   *dns.Server
	port     string
}

func NewDNSServer(zone, port string) *DNSServer {
	return &DNSServer{
		zone:    zone,
		proxmox: NewProxmoxManager(),
		port:    port,
	}
}

func (ds *DNSServer) Start() error {
	log.Printf("Starting DNS server for zone %s on port %s", ds.zone, ds.port)
	
	if err := ds.proxmox.RefreshInstances(); err != nil {
		log.Printf("Warning: Failed to refresh instances on startup: %v", err)
	}
	
	go ds.periodicRefresh()
	
	dns.HandleFunc(ds.zone, ds.handleDNSRequest)
	
	ds.server = &dns.Server{
		Addr: ":" + ds.port,
		Net:  "udp",
	}
	
	return ds.server.ListenAndServe()
}

func (ds *DNSServer) Stop() error {
	if ds.server != nil {
		return ds.server.Shutdown()
	}
	return nil
}

func (ds *DNSServer) periodicRefresh() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		if err := ds.proxmox.RefreshInstances(); err != nil {
			log.Printf("Failed to refresh instances: %v", err)
		}
	}
}

func (ds *DNSServer) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	
	for _, q := range r.Question {
		if q.Qtype == dns.TypeA && strings.HasSuffix(q.Name, ds.zone+".") {
			if answer := ds.resolveA(q.Name); answer != nil {
				m.Answer = append(m.Answer, answer)
			} else {
				m.SetRcode(r, dns.RcodeNameError)
			}
		} else {
			m.SetRcode(r, dns.RcodeNameError)
		}
	}
	
	w.WriteMsg(m)
}

func (ds *DNSServer) resolveA(name string) dns.RR {
	name = strings.TrimSuffix(name, ".")
	zoneSuffix := "." + ds.zone
	
	if !strings.HasSuffix(name, zoneSuffix) {
		return nil
	}
	
	identifier := strings.TrimSuffix(name, zoneSuffix)
	
	instance, exists := ds.proxmox.GetInstanceByIdentifier(identifier)
	if !exists {
		return nil
	}
	
	if instance.IPv4 == "" {
		return nil
	}
	
	ip := net.ParseIP(instance.IPv4)
	if ip == nil {
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
	
	log.Printf("Resolved %s to %s (instance: %s, type: %s)", name, instance.IPv4, instance.Name, instance.Type)
	return rr
}