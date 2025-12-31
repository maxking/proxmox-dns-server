package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed web/templates/index.html
var webTemplates embed.FS

type WebServer struct {
	addr   string
	zone   string
	pm     *ProxmoxManager
	server *http.Server
	tmpl   *template.Template
}

type InstanceView struct {
	ID           int
	Name         string
	Type         string
	Status       string
	IPv4         string
	FQDN         string
	ReverseNames []string
}

type PageData struct {
	Zone           string
	Instances      []InstanceView
	ReverseLookups bool
	GeneratedAt    string
}

func NewWebServer(zone, port, iface string, pm *ProxmoxManager) (*WebServer, error) {
	if pm == nil {
		return nil, fmt.Errorf("proxmox manager is required")
	}

	ifaceName := iface
	addr := ":" + port
	if iface != "" {
		iface, err := net.InterfaceByName(iface)
		if err != nil {
			return nil, fmt.Errorf("failed to find interface %s: %v", ifaceName, err)
		}

		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("failed to get addresses for interface %s: %v", ifaceName, err)
		}

		var ip net.IP
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ipNet.IP.To4() != nil {
					ip = ipNet.IP
					break
				}
			}
		}

		if ip == nil {
			return nil, fmt.Errorf("no IPv4 address found on interface %s", ifaceName)
		}

		addr = ip.String() + ":" + port
	}

	tmpl, err := template.ParseFS(webTemplates, "web/templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %v", err)
	}

	ws := &WebServer{
		addr: addr,
		zone: zone,
		pm:   pm,
		tmpl: tmpl,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)

	ws.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return ws, nil
}

func (ws *WebServer) Start() error {
	log.Printf("Starting web UI on http://%s", ws.server.Addr)
	return ws.server.ListenAndServe()
}

func (ws *WebServer) Stop() error {
	if ws.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return ws.server.Shutdown(ctx)
}

func (ws *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := ws.pm.RefreshInstances(); err != nil {
		log.Printf("Warning: Failed to refresh instances for web UI: %v", err)
	}

	reverse := r.URL.Query().Get("reverse") == "1"
	instances := ws.pm.ListInstances()
	views := make([]InstanceView, 0, len(instances))

	for _, instance := range instances {
		view := InstanceView{
			ID:     instance.ID,
			Name:   instance.Name,
			Type:   instance.Type,
			Status: instance.Status,
			IPv4:   instance.IPv4,
			FQDN:   ws.fqdnFor(instance.Name),
		}

		if reverse && instance.IPv4 != "" {
			view.ReverseNames = ws.reverseLookup(instance.IPv4)
		}

		views = append(views, view)
	}

	data := PageData{
		Zone:           ws.zone,
		Instances:      views,
		ReverseLookups: reverse,
		GeneratedAt:    time.Now().Format(time.RFC1123),
	}

	if err := ws.tmpl.Execute(w, data); err != nil {
		log.Printf("Failed to render template: %v", err)
	}
}

func (ws *WebServer) fqdnFor(name string) string {
	if ws.zone == "" || name == "" {
		return ""
	}
	return name + "." + ws.zone
}

func (ws *WebServer) reverseLookup(ip string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil {
		return nil
	}

	clean := make([]string, 0, len(names))
	for _, name := range names {
		clean = append(clean, strings.TrimSuffix(name, "."))
	}
	return clean
}
