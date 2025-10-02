package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type StaticRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   uint32 `json:"ttl"`
}

type Config struct {
	Zone          string         `json:"zone"`
	Port          string         `json:"port"`
	UpstreamDNS   string         `json:"upstreamDNS,omitempty"`
	StaticRecords []StaticRecord `json:"staticRecords,omitempty"`
	RecordsFile   string         `json:"recordsFile,omitempty"`
}

func LoadConfig() (*Config, error) {
	var zone = flag.String("zone", "", "DNS zone to serve (required)")
	var port = flag.String("port", "53", "Port to listen on")
	var configFile = flag.String("config", "", "Configuration file path")
	
	flag.Parse()
	
	config := &Config{
		Zone: *zone,
		Port: *port,
	}
	
	if *configFile != "" {
		if err := loadConfigFromFile(*configFile, config); err != nil {
			return nil, fmt.Errorf("failed to load config file: %v", err)
		}
	}
	
	if config.Zone == "" {
		return nil, fmt.Errorf("zone is required (use -zone flag or config file)")
	}
	
	return config, nil
}

func loadConfigFromFile(filename string, config *Config) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(data, config)
}