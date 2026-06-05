package types

import (
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PortMapping holds a single listen→target port pair.
type PortMapping struct {
	ListenPort int
	TargetPort int
}

// Availability mode constants for TargetInfo.Availability.
const (
	AvailabilityOnDemand = "ondemand" // start on connection, stop when idle
	AvailabilityCron     = "cron"     // start/stop via cron schedule; no on-demand start
	AvailabilityManual   = "manual"   // proxy only; no lifecycle management by this proxy
)

// TargetInfo holds information about a proxy target.
type TargetInfo struct {
	ContainerID   string
	ContainerName string
	Ports         []PortMapping
	UDPPorts      []PortMapping
	NetworkIDs    []string       // Docker only; empty in k8s mode
	AllowList     []net.IPNet    // empty = no restriction (all IPs allowed)
	BlockList     []net.IPNet    // empty = no restriction (no IPs blocked)
	IdleTimeout   *time.Duration // nil = use global default; non-nil (incl. 0) = per-container override
	StartTimeout  *time.Duration // nil = use global default; non-nil = per-container override
	Running       bool           // true if the target was running at time of inspection
	Missing       bool           // true if the container does not exist (e.g. pruned before proxy started)
	WebhookURL    string         // empty = no webhook
	Dependants    []string       // names of managed targets to start/stop alongside this one
	CronStart       string         // 5-field cron expression; "" = not scheduled
	CronStop        string         // 5-field cron expression; "" = not scheduled
	HTTPHealthCheck string         // URL to poll for readiness; "" = disabled
	HasHealthCheck  bool           // true if the container has a HEALTHCHECK configured
	TLS             bool           // true → wrap listener with TLS using shared self-signed cert
	APIKey          []string       // non-empty → require X-API-Key header matching any entry
	BasicAuth       []string       // non-empty → require Authorization: Basic matching any "user:password" entry
	DesiredReplicas int            // 0 = plain Docker container; ≥ 1 = swarm service (scale-to value)
	TraefikHosts    []string       // e.g. ["whoami.localhost:9001"] — domain:listen_port pairs for Traefik HTTP provider
	TraefikTCPHosts []string       // e.g. ["mongo.example.com:27015"] — domain:listen_port pairs for Traefik TCP SNI provider
	Availability    string         // "", "ondemand", "cron", or "manual"; "" means derived
}

// EffectiveAvailability resolves the active lifecycle management mode.
// If info.Availability is set explicitly it is returned unchanged.
// Otherwise the mode is derived: "cron" when either cron expression is set,
// "ondemand" otherwise.
func EffectiveAvailability(info TargetInfo) string {
	if info.Availability != "" {
		return info.Availability
	}
	if info.CronStart != "" || info.CronStop != "" {
		return AvailabilityCron
	}
	return AvailabilityOnDemand
}

// ParseAvailabilityLabel validates the availability label/annotation value.
// Returns "" (derive from context) if the value is absent or empty.
// Logs a warning and returns "" for unrecognised values.
func ParseAvailabilityLabel(name, raw string) string {
	v := strings.TrimSpace(raw)
	switch v {
	case "", AvailabilityOnDemand, AvailabilityCron, AvailabilityManual:
		return v
	default:
		log.Printf("container %s: ignoring invalid availability %q (must be ondemand, cron, or manual)", name, v)
		return ""
	}
}

// ContainerMemoryStat holds the memory figures for a single running container.
type ContainerMemoryStat struct {
	Name        string `json:"name"`
	MemoryUsed  int64  `json:"memory_used"`
	MemoryLimit int64  `json:"memory_limit"`
}

// TargetHandler is implemented by the proxy server to receive target updates.
type TargetHandler interface {
	RegisterTarget(info TargetInfo)
	RemoveTarget(containerID string)
	ContainerStopped(containerID string)
	ContainerStarted(containerID string)
	ContainerRemoved(containerID string)
}

// ParsePortMappings tokenises a comma-separated "<listen>:<target>" string into
// a []PortMapping. Invalid tokens are skipped with a warning log.
func ParsePortMappings(label, s string) []PortMapping {
	var out []PortMapping
	for _, token := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(token), ":", 2)
		if len(parts) != 2 {
			log.Printf("label %s: ignoring invalid token %q: expected <listen>:<target>", label, token)
			continue
		}
		lp, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		tp, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			log.Printf("label %s: ignoring invalid token %q: ports must be integers", label, token)
			continue
		}
		out = append(out, PortMapping{ListenPort: lp, TargetPort: tp})
	}
	return out
}

// ParseIPList parses a comma-delimited string of IPs and/or CIDRs into a
// slice of net.IPNet. Plain IPs are stored as /32 (IPv4) or /128 (IPv6) nets.
// Invalid entries are skipped with a warning log.
func ParseIPList(label, s string) []net.IPNet {
	var nets []net.IPNet
	for _, raw := range strings.Split(s, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		// Try CIDR first
		_, ipNet, err := net.ParseCIDR(entry)
		if err == nil {
			nets = append(nets, *ipNet)
			continue
		}
		// Try plain IP
		ip := net.ParseIP(entry)
		if ip == nil {
			log.Printf("label %s: ignoring invalid entry %q", label, entry)
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		nets = append(nets, net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets
}

// ParseDependants parses a comma-separated list of target names from the
// lazy-tcp-proxy.dependants label/annotation. Whitespace is trimmed and blank
// tokens are skipped.
func ParseDependants(s string) []string {
	var names []string
	for _, token := range strings.Split(s, ",") {
		name := strings.TrimSpace(token)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ParseStartTimeoutLabel converts a raw label value to a *time.Duration for
// the lazy-tcp-proxy.start-timeout-secs label/annotation.
// Returns nil if the value is absent, empty, non-numeric, or negative.
func ParseStartTimeoutLabel(name, raw string) *time.Duration {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("container %s: ignoring invalid start-timeout-secs %q", name, raw)
		return nil
	}
	d := time.Duration(n) * time.Second
	return &d
}

// ParseIdleTimeoutLabel converts a raw label value to a *time.Duration.
// Returns nil if the value is absent, empty, non-numeric, or negative.
// Zero is valid and means "stop immediately when all connections close".
func ParseIdleTimeoutLabel(name, raw string) *time.Duration {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("container %s: ignoring invalid idle-timeout-secs %q", name, raw)
		return nil
	}
	d := time.Duration(n) * time.Second
	return &d
}

// ParseTraefikHostSpecs parses a comma-separated list of "domain:target_port" entries
// from the lazy-tcp-proxy.traefik-hosts or traefik-tcp-hosts label.
// The port is the container's own target port; the listen port is assigned dynamically
// by the PortAllocator. Entries with an empty domain or non-integer port are skipped
// with a warning.
func ParseTraefikHostSpecs(label, s string) []TraefikHostSpec {
	var out []TraefikHostSpec
	for _, token := range strings.Split(s, ",") {
		entry := strings.TrimSpace(token)
		if entry == "" {
			continue
		}
		idx := strings.LastIndex(entry, ":")
		if idx < 1 {
			log.Printf("label %s: ignoring invalid traefik-hosts entry %q: expected domain:port", label, entry)
			continue
		}
		domain := strings.TrimSpace(entry[:idx])
		if domain == "" {
			log.Printf("label %s: ignoring traefik-hosts entry %q: domain is empty", label, entry)
			continue
		}
		tp, err := strconv.Atoi(strings.TrimSpace(entry[idx+1:]))
		if err != nil {
			log.Printf("label %s: ignoring invalid traefik-hosts entry %q: port must be an integer", label, entry)
			continue
		}
		out = append(out, TraefikHostSpec{Domain: domain, TargetPort: tp})
	}
	return out
}

// ParseAuthList splits a comma-separated label/annotation value into a slice
// of trimmed, non-empty strings. Used for api-key and basic-auth lists.
func ParseAuthList(label, s string) []string {
	var out []string
	for _, token := range strings.Split(s, ",") {
		v := strings.TrimSpace(token)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ParseHTTPHealthCheckLabel validates the http-healthcheck URL template and
// returns it unchanged (including any {{container}} placeholder).
// Substitution of {{container}} is deferred to connection time so the actual
// upstream host (IP or DNS name) can be used.
// Returns "" if the value is absent, empty, or not a valid URL structure.
func ParseHTTPHealthCheckLabel(containerName, raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	// Warn when the URL contains a {{...}} expression that isn't {{container}}.
	// This catches common misspellings like {{Container}} or {{CONTAINER}}.
	if strings.Contains(v, "{{") && !strings.Contains(v, "{{container}}") {
		log.Printf("container %s: http-healthcheck URL %q: placeholder does not match {{container}} (case-sensitive) — check spelling", containerName, v)
	}
	// Validate the URL structure by substituting a dummy hostname for the
	// placeholder so url.ParseRequestURI sees a well-formed URL.
	check := strings.ReplaceAll(v, "{{container}}", "placeholder")
	if _, err := url.ParseRequestURI(check); err != nil {
		log.Printf("container %s: ignoring invalid http-healthcheck URL %q: %v", containerName, check, err)
		return ""
	}
	return v // raw template returned; {{container}} resolved at connection time
}
