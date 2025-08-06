package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type ProxmoxInstance struct {
	ID      int    `json:"vmid"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Type    string `json:"type"`
	IPv4    string `json:"ipv4"`
}

type ProxmoxManager struct {
	instances sync.Map
	ipPrefix  string
}

var validIDRegex = regexp.MustCompile(`^\d+$`)

func NewProxmoxManager(ipPrefix string) *ProxmoxManager {
	return &ProxmoxManager{
		ipPrefix: ipPrefix,
	}
}

func (pm *ProxmoxManager) RefreshInstances() error {
	// Clear all existing entries
	pm.instances.Range(func(key, value interface{}) bool {
		pm.instances.Delete(key)
		return true
	})
	
	if err := pm.loadContainers(); err != nil {
		return fmt.Errorf("failed to load containers: %w", err)
	}
	
	if err := pm.loadVMs(); err != nil {
		return fmt.Errorf("failed to load VMs: %w", err)
	}
	
	return nil
}

func (pm *ProxmoxManager) loadContainers() error {
	cmd := exec.Command("pct", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to execute pct list: %w", err)
	}
	
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			firstLine = false
			continue
		}
		
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		
		status := fields[1]
		name := fields[2]
		
		// Only try to get IP for running containers
		if status != "running" {
			log.Printf("Debug: Container %d (%s) is %s, skipping IP detection", id, name, status)
			continue
		}
		
		ipv4, err := pm.getContainerIP(id)
		if err != nil {
			log.Printf("Warning: Failed to get IP for container %d (%s): %v", id, name, err)
			continue
		}
		
		instance := ProxmoxInstance{
			ID:      id,
			Name:    name,
			Status:  status,
			Type:    "container",
			IPv4:    ipv4,
		}
		
		pm.instances.Store(strconv.Itoa(id), instance)
		pm.instances.Store(name, instance)
	}
	
	return nil
}

func (pm *ProxmoxManager) loadVMs() error {
	cmd := exec.Command("qm", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to execute qm list: %w", err)
	}
	
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			firstLine = false
			continue
		}
		
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		
		name := fields[1]
		status := fields[2]
		
		// Only try to get IP for running VMs
		if status != "running" {
			log.Printf("Debug: VM %d (%s) is %s, skipping IP detection", id, name, status)
			continue
		}
		
		ipv4, err := pm.getVMIP(id)
		if err != nil {
			log.Printf("Warning: Failed to get IP for VM %d (%s): %v", id, name, err)
			continue
		}
		
		instance := ProxmoxInstance{
			ID:      id,
			Name:    name,
			Status:  status,
			Type:    "vm",
			IPv4:    ipv4,
		}
		
		pm.instances.Store(strconv.Itoa(id), instance)
		pm.instances.Store(name, instance)
	}
	
	return nil
}

func (pm *ProxmoxManager) getContainerIP(id int) (string, error) {
	if id <= 0 || id > 9999999 {
		return "", fmt.Errorf("invalid container ID: %d", id)
	}
	idStr := strconv.Itoa(id)
	
	// First try hostname -I
	cmd := exec.Command("pct", "exec", idStr, "--", "hostname", "-I")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Debug: Container %d - hostname -I failed: %v", id, err)
		
		// Try alternative command: ip route get 1.1.1.1 | head -1 | awk '{print $7}'
		cmd = exec.Command("pct", "exec", idStr, "--", "sh", "-c", "ip route get 1.1.1.1 2>/dev/null | head -1 | awk '{print $7}'")
		output, err = cmd.Output()
		if err != nil {
			log.Printf("Debug: Container %d - ip route get failed: %v", id, err)
			
			// Try getting IP from container config
			return pm.getContainerIPFromConfig(id)
		}
	}
	
	log.Printf("Debug: Container %d - command output: %s", id, string(output))
	return pm.filterIPv4(string(output))
}

func (pm *ProxmoxManager) getContainerIPFromConfig(id int) (string, error) {
	if id <= 0 || id > 999999999 {
		return "", fmt.Errorf("invalid container ID: %d", id)
	}
	cmd := exec.Command("pct", "config", strconv.Itoa(id))
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Debug: Container %d - pct config failed: %v", id, err)
		return "", err
	}
	
	log.Printf("Debug: Container %d - config output: %s", id, string(output))
	
	// Look for net0, net1, etc. lines with IP addresses
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "net") && strings.Contains(line, "ip=") {
			// Extract IP from line like: net0: name=eth0,bridge=vmbr0,ip=192.168.1.100/24,gw=192.168.1.1
			parts := strings.Split(line, ",")
			for _, part := range parts {
				if strings.HasPrefix(part, "ip=") {
					ipWithMask := strings.TrimPrefix(part, "ip=")
					ip := strings.Split(ipWithMask, "/")[0]
					if strings.HasPrefix(ip, pm.ipPrefix) {
						log.Printf("Debug: Container %d - Found IP in config: %s", id, ip)
						return ip, nil
					}
				}
			}
		}
	}
	
	return "", fmt.Errorf("no IP address found in container config")
}

func (pm *ProxmoxManager) getVMIP(id int) (string, error) {
	if id <= 0 || id > 999999999 {
		return "", fmt.Errorf("invalid VM ID: %d", id)
	}
	cmd := exec.Command("qm", "guest", "cmd", strconv.Itoa(id), "network-get-interfaces")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Debug: VM %d - qm guest cmd failed: %v", id, err)
		return "", err
	}
	
	log.Printf("Debug: VM %d - qm guest cmd output: %s", id, string(output))
	
	var interfaces []interface{}
	if err := json.Unmarshal(output, &interfaces); err != nil {
		log.Printf("Debug: VM %d - JSON unmarshal failed: %v", id, err)
		return "", err
	}
	
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

func (pm *ProxmoxManager) filterIPv4(output string) (string, error) {
	ips := strings.Fields(strings.TrimSpace(output))
	for _, ip := range ips {
		if net.ParseIP(ip) != nil && strings.HasPrefix(ip, pm.ipPrefix) {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no suitable IPv4 address found")
}

func (pm *ProxmoxManager) GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool) {
	value, exists := pm.instances.Load(identifier)
	if !exists {
		return ProxmoxInstance{}, false
	}
	instance, ok := value.(ProxmoxInstance)
	return instance, ok
}