package types

import (
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// PortMapping holds a single listen→target port pair.
type PortMapping struct {
	ListenPort int
	TargetPort int
}

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
	WebhookURL    string         // empty = no webhook
	Dependants    []string       // names of managed targets to start/stop alongside this one
	CronStart     string         // 5-field cron expression; "" = not scheduled
	CronStop      string         // 5-field cron expression; "" = not scheduled
}

// TargetHandler is implemented by the proxy server to receive target updates.
type TargetHandler interface {
	RegisterTarget(info TargetInfo)
	RemoveTarget(containerID string)
	ContainerStopped(containerID string)
	ContainerStarted(containerID string)
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
