package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProxmoxInstance struct {
	ID     int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
	IPv4   string `json:"ipv4"`
}

type ProxmoxAPIClient struct {
	baseURL     string
	tokenID     string
	tokenSecret string
	httpClient  *http.Client
}

type ProxmoxAPIResource struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
	Node   string `json:"node"`
}

type ProxmoxManager struct {
	instances sync.Map
	ipPrefix  string
	apiClient *ProxmoxAPIClient
}

func NewProxmoxAPIClient(baseURL, tokenID, tokenSecret string, insecure bool) (*ProxmoxAPIClient, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("api base url is required")
	}
	if !strings.HasSuffix(baseURL, "/api2/json") {
		baseURL = baseURL + "/api2/json"
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	return &ProxmoxAPIClient{
		baseURL:     baseURL,
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}, nil
}

func (c *ProxmoxAPIClient) getJSON(path string, out interface{}) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.tokenID != "" && c.tokenSecret != "" {
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", c.tokenID, c.tokenSecret))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

func (c *ProxmoxAPIClient) ListResources() ([]ProxmoxAPIResource, error) {
	var resources []ProxmoxAPIResource
	if err := c.getJSON("/cluster/resources?type=vm", &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func (c *ProxmoxAPIClient) GetLXCConfig(node string, id int) (map[string]interface{}, error) {
	var config map[string]interface{}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/config", node, id)
	if err := c.getJSON(path, &config); err != nil {
		return nil, err
	}
	return config, nil
}

func (c *ProxmoxAPIClient) GetVMInterfaces(node string, id int) ([]interface{}, error) {
	var interfaces []interface{}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", node, id)
	if err := c.getJSON(path, &interfaces); err != nil {
		return nil, err
	}
	return interfaces, nil
}

func NewProxmoxManager(ipPrefix string, apiClient *ProxmoxAPIClient) *ProxmoxManager {
	return &ProxmoxManager{
		ipPrefix:  ipPrefix,
		apiClient: apiClient,
	}
}

func (pm *ProxmoxManager) RefreshInstances() error {
	// Clear all existing entries
	pm.instances.Range(func(key, value interface{}) bool {
		pm.instances.Delete(key)
		return true
	})

	if pm.apiClient == nil {
		return fmt.Errorf("api client is required")
	}

	if err := pm.loadInstancesFromAPI(); err != nil {
		return fmt.Errorf("failed to load instances via API: %w", err)
	}
	return nil
}

func (pm *ProxmoxManager) loadInstancesFromAPI() error {
	resources, err := pm.apiClient.ListResources()
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if resource.VMID <= 0 {
			continue
		}

		if resource.Status != "running" {
			log.Printf("Debug: Instance %d (%s) is %s, skipping IP detection", resource.VMID, resource.Name, resource.Status)
			continue
		}

		var instanceType string
		var ipv4 string

		switch resource.Type {
		case "lxc":
			instanceType = "container"
			config, err := pm.apiClient.GetLXCConfig(resource.Node, resource.VMID)
			if err != nil {
				log.Printf("Warning: Failed to get LXC config for %d (%s): %v", resource.VMID, resource.Name, err)
				continue
			}
			ipv4, err = pm.getContainerIPFromConfigMap(resource.VMID, config)
		case "qemu":
			instanceType = "vm"
			interfaces, err := pm.apiClient.GetVMInterfaces(resource.Node, resource.VMID)
			if err != nil {
				log.Printf("Warning: Failed to get VM interfaces for %d (%s): %v", resource.VMID, resource.Name, err)
				continue
			}
			ipv4, err = pm.findIPv4FromInterfaces(resource.VMID, interfaces)
		default:
			continue
		}

		if err != nil {
			log.Printf("Warning: Failed to get IP for %s %d (%s): %v", instanceType, resource.VMID, resource.Name, err)
			continue
		}

		instance := ProxmoxInstance{
			ID:     resource.VMID,
			Name:   resource.Name,
			Status: resource.Status,
			Type:   instanceType,
			IPv4:   ipv4,
		}

		pm.instances.Store(strconv.Itoa(resource.VMID), instance)
		pm.instances.Store(resource.Name, instance)
	}

	return nil
}

func (pm *ProxmoxManager) findIPv4FromInterfaces(id int, interfaces []interface{}) (string, error) {
	log.Printf("Debug: VM %d - Found %d network interfaces", id, len(interfaces))
	for i, iface := range interfaces {
		if ifaceMap, ok := iface.(map[string]interface{}); ok {
			ifaceName := "unknown"
			if name, ok := ifaceMap["name"].(string); ok {
				ifaceName = name
			}
			log.Printf("Debug: VM %d - Interface %d (%s)", id, i, ifaceName)

			if ipAddresses, ok := ifaceMap["ip-addresses"].([]interface{}); ok {
				log.Printf("Debug: VM %d - Interface %s has %d IP addresses", id, ifaceName, len(ipAddresses))
				for j, ip := range ipAddresses {
					if ipMap, ok := ip.(map[string]interface{}); ok {
						log.Printf("Debug: VM %d - IP %d: %+v", id, j, ipMap)
						if ipType, ok := ipMap["ip-address-type"].(string); ok && ipType == "ipv4" {
							if ipAddr, ok := ipMap["ip-address"].(string); ok {
								log.Printf("Debug: VM %d - Found IPv4: %s", id, ipAddr)
								if strings.HasPrefix(ipAddr, pm.ipPrefix) {
									log.Printf("Debug: VM %d - Using IPv4: %s", id, ipAddr)
									return ipAddr, nil
								}
							}
						}
					}
				}
			} else {
				log.Printf("Debug: VM %d - Interface %s has no ip-addresses field", id, ifaceName)
			}
		}
	}

	log.Printf("Debug: VM %d - No suitable IPv4 address found", id)
	return "", fmt.Errorf("no suitable IPv4 address found")
}

func (pm *ProxmoxManager) getContainerIPFromConfigMap(id int, config map[string]interface{}) (string, error) {
	for key, value := range config {
		if !strings.HasPrefix(key, "net") {
			continue
		}
		netConfig, ok := value.(string)
		if !ok {
			continue
		}
		if ip, ok := pm.parseIPFromNetConfigValue(id, netConfig); ok {
			return ip, nil
		}
	}

	return "", fmt.Errorf("no IP address found in container config")
}

func (pm *ProxmoxManager) parseIPFromNetConfigValue(id int, netConfig string) (string, bool) {
	parts := strings.Split(netConfig, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "ip=") {
			continue
		}
		ipWithMask := strings.TrimPrefix(part, "ip=")
		if ipWithMask == "" || ipWithMask == "dhcp" {
			continue
		}
		ip := strings.Split(ipWithMask, "/")[0]
		if strings.HasPrefix(ip, pm.ipPrefix) {
			log.Printf("Debug: Container %d - Found IP in config: %s", id, ip)
			return ip, true
		}
	}

	return "", false
}

func (pm *ProxmoxManager) GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool) {
	value, exists := pm.instances.Load(identifier)
	if !exists {
		return ProxmoxInstance{}, false
	}
	instance, ok := value.(ProxmoxInstance)
	return instance, ok
}
