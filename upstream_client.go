package main

import (
	"log"
	"time"

	"github.com/miekg/dns"
)

type UpstreamClient struct {
	server  string
	client  *dns.Client
	timeout time.Duration
}

func NewUpstreamClient(server string) *UpstreamClient {
	if server == "" {
		return nil
	}

	return &UpstreamClient{
		server: server,
		client: &dns.Client{
			Net:     "udp",
			Timeout: 5 * time.Second,
		},
		timeout: 5 * time.Second,
	}
}

func (uc *UpstreamClient) Query(r *dns.Msg) *dns.Msg {
	if uc == nil {
		return nil
	}

	log.Printf("Forwarding DNS query to upstream server %s", uc.server)

	resp, _, err := uc.client.Exchange(r, uc.server)
	if err != nil {
		log.Printf("Failed to query upstream DNS server %s: %v", uc.server, err)
		return nil
	}

	if resp == nil {
		log.Printf("No response from upstream DNS server %s", uc.server)
		return nil
	}

	log.Printf("Received response from upstream server with %d answers", len(resp.Answer))
	return resp
}

func (uc *UpstreamClient) IsConfigured() bool {
	return uc != nil && uc.server != ""
}
