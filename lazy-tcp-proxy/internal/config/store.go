package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	cron "github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

// DynamicConfig is the top-level structure of the YAML config file.
type DynamicConfig struct {
	Services []ServiceEntry `yaml:"services" json:"services"`
}

// ServiceEntry mirrors every configurable field of types.TargetInfo.
// All fields except Name are optional.
type ServiceEntry struct {
	Name             string   `yaml:"name"                        json:"name"`
	Ports            []string `yaml:"ports,omitempty"             json:"ports,omitempty"`
	UDPPorts         []string `yaml:"udp_ports,omitempty"         json:"udp_ports,omitempty"`
	AllowList        []string `yaml:"allow_list,omitempty"        json:"allow_list,omitempty"`
	BlockList        []string `yaml:"block_list,omitempty"        json:"block_list,omitempty"`
	IdleTimeoutSecs  *int     `yaml:"idle_timeout_secs,omitempty" json:"idle_timeout_secs,omitempty"`
	StartTimeoutSecs *int     `yaml:"start_timeout_secs,omitempty" json:"start_timeout_secs,omitempty"`
	WebhookURL       string   `yaml:"webhook_url,omitempty"       json:"webhook_url,omitempty"`
	Dependants       []string `yaml:"dependants,omitempty"        json:"dependants,omitempty"`
	CronStart        string   `yaml:"cron_start,omitempty"        json:"cron_start,omitempty"`
	CronStop         string   `yaml:"cron_stop,omitempty"         json:"cron_stop,omitempty"`
	HTTPHealthCheck  string   `yaml:"http_healthcheck,omitempty"  json:"http_healthcheck,omitempty"`
	HTTPS            bool     `yaml:"https,omitempty"             json:"https,omitempty"`
	APIKey           string   `yaml:"api_key,omitempty"           json:"api_key,omitempty"`
	Scale            *int     `yaml:"scale,omitempty"             json:"scale,omitempty"`
}

// Store manages a YAML config file on disk and the in-memory config it represents.
type Store struct {
	mu     sync.RWMutex
	path   string
	config DynamicConfig
}

// New creates a Store for the given file path. Call Load() to read the file.
func New(path string) *Store {
	return &Store{path: path}
}

// Load reads and parses the YAML config file. If the file does not exist, an
// empty placeholder is created and a warning is logged. Returns an error only
// for unexpected I/O or parse failures.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		placeholder := []byte(`services:
#  - name: "my-container"
#    ports:
#      - "9000:80"
#    udp_ports:
#      - "5353:53"
#    allow_list: ["192.168.0.0/24"]
#    block_list: ["10.0.0.1"]
#    idle_timeout_secs: 60
#    start_timeout_secs: 30
#    webhook_url: "https://example.com/hook"
#    dependants: ["other-service"]
#    cron_start: "0 9 * * 1-5"
#    cron_stop:  "0 17 * * 1-5"
#    http_healthcheck: "http://{{container}}:8080/health"
#    https: true
#    api_key: "your-secret-key"
`)
		if werr := os.WriteFile(s.path, placeholder, 0o644); werr != nil {
			log.Printf("config: could not create placeholder at %s: %v", s.path, werr)
		} else {
			log.Printf("config: no config file found at %s — created placeholder with example config", s.path)
		}
		s.mu.Lock()
		s.config = DynamicConfig{}
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: read %s: %w", s.path, err)
	}

	var cfg DynamicConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", s.path, err)
	}

	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return nil
}

// Save validates cfg, writes it as YAML to the config file atomically, and
// updates the in-memory config. Returns an error if validation fails or the
// write fails; the in-memory config is unchanged on error.
func (s *Store) Save(cfg DynamicConfig) error {
	if errs := validate(cfg); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("config validation failed: %s", strings.Join(msgs, "; "))
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("config: write temp file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("config: rename %s → %s: %w", tmp, s.path, err)
	}

	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return nil
}

// Get returns a copy of the current in-memory config.
func (s *Store) Get() DynamicConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// Apply merges the YAML config over the list of backend-discovered targets.
//
// For each ServiceEntry in the config:
//   - If its Name matches a discovered target (by ContainerName), the YAML
//     entry fully replaces that target's config. ContainerID, NetworkIDs,
//     Running, and HasHealthCheck are preserved from the discovered target.
//   - If no discovered target matches, a new target is added using
//     defaultID(name) as the ContainerID.
//
// Invalid entries are skipped and their errors returned; valid entries are
// always applied.
func (s *Store) Apply(discovered []types.TargetInfo, defaultID func(string) string) ([]types.TargetInfo, []error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	// Build name → slice-index map.
	byName := make(map[string]int, len(discovered))
	for i, t := range discovered {
		byName[t.ContainerName] = i
	}

	result := make([]types.TargetInfo, len(discovered))
	copy(result, discovered)

	var errs []error

	for _, entry := range cfg.Services {
		info, err := entryToTargetInfo(entry)
		if err != nil {
			errs = append(errs, fmt.Errorf("service %q: %w", entry.Name, err))
			continue
		}

		if idx, ok := byName[entry.Name]; ok {
			// Preserve backend-managed fields from the discovered target.
			info.ContainerID = discovered[idx].ContainerID
			info.NetworkIDs = discovered[idx].NetworkIDs
			info.Running = discovered[idx].Running
			info.HasHealthCheck = discovered[idx].HasHealthCheck
			info.DesiredReplicas = discovered[idx].DesiredReplicas
			result[idx] = info
		} else {
			// YAML-only entry: assign a backend-appropriate container ID.
			info.ContainerID = defaultID(entry.Name)
			result = append(result, info)
		}
	}

	return result, errs
}

// entryToTargetInfo converts a ServiceEntry into a types.TargetInfo.
func entryToTargetInfo(entry ServiceEntry) (types.TargetInfo, error) {
	info := types.TargetInfo{
		ContainerName: entry.Name,
	}

	if len(entry.Ports) > 0 {
		info.Ports = types.ParsePortMappings("ports", strings.Join(entry.Ports, ","))
	}
	if len(entry.UDPPorts) > 0 {
		info.UDPPorts = types.ParsePortMappings("udp_ports", strings.Join(entry.UDPPorts, ","))
	}
	if len(info.Ports) == 0 && len(info.UDPPorts) == 0 {
		return types.TargetInfo{}, fmt.Errorf("must specify at least one port in ports or udp_ports")
	}

	if len(entry.AllowList) > 0 {
		info.AllowList = types.ParseIPList("allow_list", strings.Join(entry.AllowList, ","))
	}
	if len(entry.BlockList) > 0 {
		info.BlockList = types.ParseIPList("block_list", strings.Join(entry.BlockList, ","))
	}

	if entry.IdleTimeoutSecs != nil {
		d := time.Duration(*entry.IdleTimeoutSecs) * time.Second
		info.IdleTimeout = &d
	}
	if entry.StartTimeoutSecs != nil {
		d := time.Duration(*entry.StartTimeoutSecs) * time.Second
		info.StartTimeout = &d
	}

	if entry.WebhookURL != "" {
		if _, err := url.ParseRequestURI(entry.WebhookURL); err != nil {
			return types.TargetInfo{}, fmt.Errorf("invalid webhook_url %q: %w", entry.WebhookURL, err)
		}
		info.WebhookURL = entry.WebhookURL
	}

	info.Dependants = entry.Dependants
	info.CronStart = entry.CronStart
	info.CronStop = entry.CronStop
	info.HTTPHealthCheck = entry.HTTPHealthCheck
	info.HTTPS = entry.HTTPS
	info.APIKey = entry.APIKey

	if entry.Scale != nil && *entry.Scale >= 1 {
		info.DesiredReplicas = *entry.Scale
	}

	return info, nil
}

// validate checks all service entries in cfg and returns any errors found.
// It does not stop at the first error.
func validate(cfg DynamicConfig) []error {
	var errs []error
	cronParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, entry := range cfg.Services {
		if entry.Name == "" {
			errs = append(errs, fmt.Errorf("service entry is missing required field 'name'"))
		}
		if entry.CronStart != "" {
			if _, err := cronParser.Parse(entry.CronStart); err != nil {
				errs = append(errs, fmt.Errorf("service %q: invalid cron_start %q: %w", entry.Name, entry.CronStart, err))
			}
		}
		if entry.CronStop != "" {
			if _, err := cronParser.Parse(entry.CronStop); err != nil {
				errs = append(errs, fmt.Errorf("service %q: invalid cron_stop %q: %w", entry.Name, entry.CronStop, err))
			}
		}
		if entry.WebhookURL != "" {
			if _, err := url.ParseRequestURI(entry.WebhookURL); err != nil {
				errs = append(errs, fmt.Errorf("service %q: invalid webhook_url %q: %w", entry.Name, entry.WebhookURL, err))
			}
		}
	}
	return errs
}

// TargetCollector implements types.TargetHandler by collecting targets into a
// slice. Used to intercept Discover() calls before applying the config overlay.
type TargetCollector struct {
	mu      sync.Mutex
	targets []types.TargetInfo
}

func (c *TargetCollector) RegisterTarget(info types.TargetInfo) {
	c.mu.Lock()
	c.targets = append(c.targets, info)
	c.mu.Unlock()
}
func (c *TargetCollector) RemoveTarget(_ string)      {}
func (c *TargetCollector) ContainerStopped(_ string)  {}
func (c *TargetCollector) ContainerStarted(_ string)  {}

// Targets returns a copy of all collected targets.
func (c *TargetCollector) Targets() []types.TargetInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]types.TargetInfo, len(c.targets))
	copy(out, c.targets)
	return out
}
