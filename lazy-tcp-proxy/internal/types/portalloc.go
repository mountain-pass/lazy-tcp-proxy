package types

import (
	"fmt"
	"sync"
)

// TraefikHostSpec is a parsed traefik_hosts / traefik_tcp_hosts label entry.
// Domain is the hostname; TargetPort is the container's own listening port.
type TraefikHostSpec struct {
	Domain     string
	TargetPort int
}

// PortAllocator assigns sequential listen ports to traefik host entries,
// starting from a configurable base port. It tracks all claimed ports (from
// explicit lazy-tcp-proxy.ports / udp-ports mappings and prior allocations)
// and skips them when assigning new ones.
//
// Allocated ports are never freed, keeping the domain→port mapping stable
// while Traefik refreshes its config.
type PortAllocator struct {
	mu       sync.Mutex
	base     int
	claimed  map[int]struct{}
	assigned map[string]int // domain → assigned listen port
}

// NewPortAllocator creates a PortAllocator that starts assigning ports at base.
func NewPortAllocator(base int) *PortAllocator {
	return &PortAllocator{
		base:     base,
		claimed:  make(map[int]struct{}),
		assigned: make(map[string]int),
	}
}

// ClaimPorts marks the listen ports in mappings as taken so the allocator
// will not assign them dynamically. Call this with the explicit ports parsed
// from lazy-tcp-proxy.ports and lazy-tcp-proxy.udp-ports labels before
// calling AllocateForHosts.
func (a *PortAllocator) ClaimPorts(mappings []PortMapping) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range mappings {
		a.claimed[m.ListenPort] = struct{}{}
	}
}

// AllocatePortMappings resolves a slice of TraefikHostSpec into
// "domain:listen_port" strings and corresponding PortMapping entries.
// Append the returned mappings to TargetInfo.Ports so the proxy server
// creates TCP listeners for dynamically assigned ports.
//
// Each unique domain receives one listen port. If a domain was previously
// allocated it reuses the same port (stable across re-registrations).
// Specs with an empty Domain are silently skipped.
func (a *PortAllocator) AllocatePortMappings(specs []TraefikHostSpec) ([]string, []PortMapping) {
	a.mu.Lock()
	defer a.mu.Unlock()
	hosts := make([]string, 0, len(specs))
	mappings := make([]PortMapping, 0, len(specs))
	for _, spec := range specs {
		if spec.Domain == "" {
			continue
		}
		var p int
		if existing, ok := a.assigned[spec.Domain]; ok {
			p = existing
		} else {
			p = a.nextFree()
			a.assigned[spec.Domain] = p
			a.claimed[p] = struct{}{}
		}
		hosts = append(hosts, fmt.Sprintf("%s:%d", spec.Domain, p))
		mappings = append(mappings, PortMapping{ListenPort: p, TargetPort: spec.TargetPort})
	}
	return hosts, mappings
}

// AllocateForHosts resolves a slice of TraefikHostSpec into
// "domain:listen_port" strings suitable for TargetInfo.TraefikHosts.
// Use AllocatePortMappings when you also need PortMapping entries for the proxy.
func (a *PortAllocator) AllocateForHosts(specs []TraefikHostSpec) []string {
	hosts, _ := a.AllocatePortMappings(specs)
	return hosts
}

func (a *PortAllocator) nextFree() int {
	p := a.base
	for {
		if _, taken := a.claimed[p]; !taken {
			return p
		}
		p++
	}
}
