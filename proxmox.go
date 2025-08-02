package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

type ProxmoxInstance struct {
	ID      int    `json:"vmid"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Type    string `json:"type"`
	IPv4    string `json:"ipv4"`
}

type ProxmoxManager struct {
	instances map[string]ProxmoxInstance
}

func NewProxmoxManager() *ProxmoxManager {
	return &ProxmoxManager{
		instances: make(map[string]ProxmoxInstance),
	}
}

func (pm *ProxmoxManager) RefreshInstances() error {
	pm.instances = make(map[string]ProxmoxInstance)
	
	if err := pm.loadContainers(); err != nil {
		return fmt.Errorf("failed to load containers: %v", err)
	}
	
	if err := pm.loadVMs(); err != nil {
		return fmt.Errorf("failed to load VMs: %v", err)
	}
	
	return nil
}

func (pm *ProxmoxManager) loadContainers() error {
	cmd := exec.Command("pct", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to execute pct list: %v", err)
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
		
		ipv4, err := pm.getContainerIP(id)
		if err != nil {
			continue
		}
		
		instance := ProxmoxInstance{
			ID:      id,
			Name:    name,
			Status:  status,
			Type:    "container",
			IPv4:    ipv4,
		}
		
		pm.instances[strconv.Itoa(id)] = instance
		pm.instances[name] = instance
	}
	
	return nil
}

func (pm *ProxmoxManager) loadVMs() error {
	cmd := exec.Command("qm", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to execute qm list: %v", err)
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
		
		ipv4, err := pm.getVMIP(id)
		if err != nil {
			continue
		}
		
		instance := ProxmoxInstance{
			ID:      id,
			Name:    name,
			Status:  status,
			Type:    "vm",
			IPv4:    ipv4,
		}
		
		pm.instances[strconv.Itoa(id)] = instance
		pm.instances[name] = instance
	}
	
	return nil
}

func (pm *ProxmoxManager) getContainerIP(id int) (string, error) {
	cmd := exec.Command("pct", "exec", strconv.Itoa(id), "--", "hostname", "-I")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	
	return pm.filterIPv4(string(output))
}

func (pm *ProxmoxManager) getVMIP(id int) (string, error) {
	cmd := exec.Command("qm", "guest", "cmd", strconv.Itoa(id), "network-get-interfaces")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", err
	}
	
	if return_val, ok := result["return"].([]interface{}); ok {
		for _, iface := range return_val {
			if ifaceMap, ok := iface.(map[string]interface{}); ok {
				if ipAddresses, ok := ifaceMap["ip-addresses"].([]interface{}); ok {
					for _, ip := range ipAddresses {
						if ipMap, ok := ip.(map[string]interface{}); ok {
							if ipType, ok := ipMap["ip-address-type"].(string); ok && ipType == "ipv4" {
								if ipAddr, ok := ipMap["ip-address"].(string); ok {
									if strings.HasPrefix(ipAddr, "192.168.") {
										return ipAddr, nil
									}
								}
							}
						}
					}
				}
			}
		}
	}
	
	return "", fmt.Errorf("no suitable IPv4 address found")
}

func (pm *ProxmoxManager) filterIPv4(output string) (string, error) {
	ips := strings.Fields(strings.TrimSpace(output))
	for _, ip := range ips {
		if net.ParseIP(ip) != nil && strings.HasPrefix(ip, "192.168.") {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no suitable IPv4 address found")
}

func (pm *ProxmoxManager) GetInstanceByIdentifier(identifier string) (ProxmoxInstance, bool) {
	instance, exists := pm.instances[identifier]
	return instance, exists
}