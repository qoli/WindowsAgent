// Package assistgui owns the user-facing WindowsAgent installation and access
// summary consumed by the native Assist GUI.
package assistgui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	TailscaleDisabled = "DISABLED"
	TailscaleOnline   = "ONLINE"
	TailscaleFailed   = "FAILED"
	TailscaleStarting = "STARTING"
	TailscaleStopping = "STOPPING"
)

type Snapshot struct {
	Installed        bool
	AgentHealthy     bool
	AgentVersion     string
	AgentListen      string
	LANEndpoints     []Endpoint
	Tailscale        TailscaleSnapshot
	AgentHealthError string
}

type Endpoint struct {
	Interface string
	IP        string
	URL       string
}

type TailscaleSnapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	Enabled       bool      `json:"enabled"`
	State         string    `json:"state"`
	IPv4          string    `json:"ipv4,omitempty"`
	IPv6          string    `json:"ipv6,omitempty"`
	Error         string    `json:"error,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ProcessID     int       `json:"processId"`
	Generation    string    `json:"generation"`
}

type Inspector struct {
	DataDir    string
	AgentURL   string
	Port       string
	Client     *http.Client
	Interfaces func() ([]net.Interface, error)
}

func (i Inspector) Snapshot(ctx context.Context) (Snapshot, error) {
	if i.DataDir == "" || !filepath.IsAbs(i.DataDir) {
		return Snapshot{}, errors.New("assist data directory must be absolute")
	}
	if i.AgentURL == "" {
		return Snapshot{}, errors.New("agent health URL is required")
	}
	if i.Port == "" {
		return Snapshot{}, errors.New("agent port is required")
	}
	client := i.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	interfaces := i.Interfaces
	if interfaces == nil {
		interfaces = net.Interfaces
	}
	snapshot := Snapshot{Installed: installed(i.DataDir), Tailscale: LoadTailscaleStatus(i.DataDir)}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, i.AgentURL, nil)
	if err != nil {
		return Snapshot{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		snapshot.AgentHealthError = err.Error()
		return snapshot, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		snapshot.AgentHealthError = fmt.Sprintf("health returned HTTP %d", response.StatusCode)
		return snapshot, nil
	}
	var health struct {
		Status  string `json:"status"`
		Service string `json:"service"`
		Version string `json:"version"`
		Listen  string `json:"listen"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&health); err != nil {
		snapshot.AgentHealthError = "decode health: " + err.Error()
		return snapshot, nil
	}
	if health.Status != "ok" {
		snapshot.AgentHealthError = fmt.Sprintf("health status is %q", health.Status)
		return snapshot, nil
	}
	if health.Service != "windows-capture-agent" {
		snapshot.AgentHealthError = fmt.Sprintf("health service is %q", health.Service)
		return snapshot, nil
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		snapshot.AgentHealthError = "health response contains multiple JSON values"
		return snapshot, nil
	}
	snapshot.AgentHealthy = true
	snapshot.AgentVersion = health.Version
	snapshot.AgentListen = health.Listen
	listenHost, listenPort, err := net.SplitHostPort(health.Listen)
	if err != nil || listenPort == "" {
		snapshot.AgentHealthy = false
		snapshot.AgentHealthError = fmt.Sprintf("health listen address is invalid: %q", health.Listen)
		return snapshot, nil
	}
	endpoints, err := lanEndpoints(interfaces, listenHost, listenPort)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.LANEndpoints = endpoints
	return snapshot, nil
}

func installed(dataDir string) bool {
	info, err := os.Stat(filepath.Join(dataDir, "bin", "windows-capture-agent.exe"))
	return err == nil && info.Mode().IsRegular()
}

func LoadTailscaleStatus(dataDir string) TailscaleSnapshot {
	path := filepath.Join(dataDir, "tailscale", "status.json")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: false, State: TailscaleDisabled}
	}
	if err != nil {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: err.Error()}
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: "adapter status must be a regular file no larger than 64 KiB"}
	}
	file, err := os.Open(path)
	if err != nil {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: err.Error()}
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var status TailscaleSnapshot
	if err := decoder.Decode(&status); err != nil {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: "decode adapter status: " + err.Error()}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: "adapter status contains multiple JSON values"}
	}
	if status.SchemaVersion != 1 {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: fmt.Sprintf("unsupported adapter status schemaVersion %d", status.SchemaVersion)}
	}
	if !status.Enabled {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: false, State: TailscaleDisabled, UpdatedAt: status.UpdatedAt}
	}
	if status.ProcessID <= 0 || len(status.Generation) != 32 {
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: "adapter status process identity is invalid"}
	}
	switch status.State {
	case TailscaleOnline, TailscaleFailed, TailscaleStarting, TailscaleStopping:
	default:
		return TailscaleSnapshot{SchemaVersion: 1, Enabled: true, State: TailscaleFailed, Error: fmt.Sprintf("invalid adapter state %q", status.State)}
	}
	return status
}

func lanEndpoints(list func() ([]net.Interface, error), listenHost, port string) ([]Endpoint, error) {
	if listenHost == "127.0.0.1" {
		return nil, nil
	}
	var requiredIP netip.Addr
	if listenHost != "0.0.0.0" {
		parsed, err := netip.ParseAddr(listenHost)
		if err != nil || !parsed.Is4() || !parsed.IsPrivate() {
			return nil, fmt.Errorf("agent listener does not expose a private LAN IPv4 address: %q", listenHost)
		}
		requiredIP = parsed
	}
	interfaces, err := list()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	var endpoints []Endpoint
	seen := make(map[netip.Addr]struct{})
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for %s: %w", iface.Name, err)
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr().Unmap()
			if !ip.Is4() || !ip.IsPrivate() {
				continue
			}
			if requiredIP.IsValid() && ip != requiredIP {
				continue
			}
			if _, duplicate := seen[ip]; duplicate {
				continue
			}
			seen[ip] = struct{}{}
			endpoints = append(endpoints, Endpoint{Interface: iface.Name, IP: ip.String(), URL: "http://" + net.JoinHostPort(ip.String(), port)})
		}
	}
	sort.Slice(endpoints, func(a, b int) bool {
		if endpoints[a].Interface == endpoints[b].Interface {
			return endpoints[a].IP < endpoints[b].IP
		}
		return endpoints[a].Interface < endpoints[b].Interface
	})
	return endpoints, nil
}
