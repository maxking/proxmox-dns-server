package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

type StaticRecordManager struct {
	records map[string][]StaticRecord
	mutex   sync.RWMutex
}

type RecordsFile struct {
	Records []StaticRecord `json:"records"`
}

func NewStaticRecordManager() *StaticRecordManager {
	return &StaticRecordManager{
		records: make(map[string][]StaticRecord),
	}
}

func (srm *StaticRecordManager) LoadRecords(config *Config) error {
	srm.mutex.Lock()
	defer srm.mutex.Unlock()

	srm.records = make(map[string][]StaticRecord)

	for _, record := range config.StaticRecords {
		if err := srm.validateRecord(&record); err != nil {
			log.Printf("Warning: Invalid static record %s: %v", record.Name, err)
			continue
		}
		srm.addRecord(record, config.Zone)
	}

	if config.RecordsFile != "" {
		if err := srm.loadFromFile(config.RecordsFile, config.Zone); err != nil {
			return fmt.Errorf("failed to load records file: %v", err)
		}
	}

	log.Printf("Loaded %d static record entries", len(srm.records))
	return nil
}

func (srm *StaticRecordManager) loadFromFile(filename, zone string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	var recordsFile RecordsFile
	if err := json.Unmarshal(data, &recordsFile); err != nil {
		return err
	}

	for _, record := range recordsFile.Records {
		if err := srm.validateRecord(&record); err != nil {
			log.Printf("Warning: Invalid record in file %s: %v", record.Name, err)
			continue
		}
		srm.addRecord(record, zone)
	}

	return nil
}

func (srm *StaticRecordManager) addRecord(record StaticRecord, zone string) {
	primaryKey := normalizeRecordKey(record.Name)
	if primaryKey == "" {
		return
	}

	srm.records[primaryKey] = append(srm.records[primaryKey], record)

	zone = strings.TrimSpace(zone)
	zone = strings.TrimSuffix(zone, ".")
	zone = strings.ToLower(zone)
	if zone == "" {
		return
	}

	if strings.Contains(primaryKey, ".") {
		return
	}

	fqdnKey := normalizeRecordKey(primaryKey + "." + zone)
	srm.records[fqdnKey] = append(srm.records[fqdnKey], record)
}

func normalizeRecordKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	name = strings.TrimSuffix(name, ".")
	return strings.ToLower(name)
}

func (srm *StaticRecordManager) validateRecord(record *StaticRecord) error {
	if record.Name == "" {
		return fmt.Errorf("record name cannot be empty")
	}

	record.Type = strings.ToUpper(record.Type)
	switch record.Type {
	case "A":
		if net.ParseIP(record.Value) == nil {
			return fmt.Errorf("invalid IPv4 address: %s", record.Value)
		}
	case "AAAA":
		if net.ParseIP(record.Value) == nil {
			return fmt.Errorf("invalid IPv6 address: %s", record.Value)
		}
	case "CNAME", "TXT":
		if record.Value == "" {
			return fmt.Errorf("value cannot be empty for %s record", record.Type)
		}
	case "MX":
		parts := strings.Fields(record.Value)
		if len(parts) != 2 {
			return fmt.Errorf("MX record must have format 'priority hostname'")
		}
		if _, err := strconv.Atoi(parts[0]); err != nil {
			return fmt.Errorf("invalid MX priority: %s", parts[0])
		}
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	if record.TTL == 0 {
		record.TTL = 300
	}

	return nil
}

func (srm *StaticRecordManager) ResolveRecord(identifier, queryName, qtype string) []dns.RR {
	srm.mutex.RLock()
	defer srm.mutex.RUnlock()

	lookupKeys := []string{
		normalizeRecordKey(identifier),
		normalizeRecordKey(queryName),
	}

	seen := make(map[string]struct{})
	var records []StaticRecord
	for _, key := range lookupKeys {
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if recs, ok := srm.records[key]; ok {
			records = append(records, recs...)
		}
	}

	if len(records) == 0 {
		return nil
	}

	owner := queryName
	if owner == "" {
		owner = identifier
	}
	owner = dns.Fqdn(owner)

	var answers []dns.RR
	requestedType := strings.ToUpper(qtype)
	for _, record := range records {
		if strings.ToUpper(record.Type) != requestedType {
			continue
		}

		rr := srm.createDNSRecord(owner, record)
		if rr != nil {
			answers = append(answers, rr)
		}
	}

	return answers
}

func (srm *StaticRecordManager) createDNSRecord(owner string, record StaticRecord) dns.RR {
	fqdn := dns.Fqdn(owner)

	header := dns.RR_Header{
		Name:  fqdn,
		Class: dns.ClassINET,
		Ttl:   record.TTL,
	}

	switch strings.ToUpper(record.Type) {
	case "A":
		header.Rrtype = dns.TypeA
		return &dns.A{
			Hdr: header,
			A:   net.ParseIP(record.Value),
		}
	case "AAAA":
		header.Rrtype = dns.TypeAAAA
		return &dns.AAAA{
			Hdr:  header,
			AAAA: net.ParseIP(record.Value),
		}
	case "CNAME":
		header.Rrtype = dns.TypeCNAME
		target := record.Value
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.CNAME{
			Hdr:    header,
			Target: target,
		}
	case "TXT":
		header.Rrtype = dns.TypeTXT
		return &dns.TXT{
			Hdr: header,
			Txt: []string{record.Value},
		}
	case "MX":
		header.Rrtype = dns.TypeMX
		parts := strings.Fields(record.Value)
		priority, _ := strconv.Atoi(parts[0])
		mx := parts[1]
		if !strings.HasSuffix(mx, ".") {
			mx += "."
		}
		return &dns.MX{
			Hdr:        header,
			Preference: uint16(priority),
			Mx:         mx,
		}
	}

	return nil
}

func (srm *StaticRecordManager) HasRecord(name string) bool {
	srm.mutex.RLock()
	defer srm.mutex.RUnlock()

	key := normalizeRecordKey(name)
	_, exists := srm.records[key]
	return exists
}
