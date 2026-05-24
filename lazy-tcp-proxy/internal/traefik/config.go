package traefik

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Snapshot is the minimal per-container info needed to build the Traefik config.
// Defined here so this package does not import the proxy package.
type Snapshot struct {
	ContainerName string
	TCPPorts      []int    // all TCP listen ports for this container
	UDPPorts      []int    // all UDP listen ports for this container
	TraefikHosts  []string // domain:port pairs for HTTP section, e.g. ["whoami.localhost:9001"]
}

// TraefikConfig is the top-level Traefik HTTP provider dynamic config payload.
type TraefikConfig struct {
	HTTP HTTPConfig `json:"http"`
	TCP  *TCPConfig `json:"tcp,omitempty"`
	UDP  *UDPConfig `json:"udp,omitempty"`
}

// HTTPConfig holds the routers and services sections of the HTTP provider payload.
type HTTPConfig struct {
	Routers  map[string]HTTPRouter  `json:"routers"`
	Services map[string]HTTPService `json:"services"`
}

// RouterTLS holds the TLS options for a Traefik HTTP router.
type RouterTLS struct {
	CertResolver string `json:"certResolver,omitempty"`
}

// HTTPRouter is a single Traefik HTTP router entry.
type HTTPRouter struct {
	Rule        string     `json:"rule"`
	Service     string     `json:"service"`
	EntryPoints []string   `json:"entryPoints,omitempty"`
	TLS         *RouterTLS `json:"tls,omitempty"`
}

// HTTPService is a single Traefik HTTP service entry.
type HTTPService struct {
	LoadBalancer LoadBalancer `json:"loadBalancer"`
}

// LoadBalancer holds the upstream server list for a Traefik HTTP service.
type LoadBalancer struct {
	Servers []Server `json:"servers"`
}

// Server is a single upstream server URL (HTTP).
type Server struct {
	URL string `json:"url"`
}

// TCPConfig holds the routers and services for the Traefik TCP provider section.
type TCPConfig struct {
	Routers  map[string]TCPRouter  `json:"routers"`
	Services map[string]TCPService `json:"services"`
}

// TCPRouter is a single Traefik TCP router entry.
type TCPRouter struct {
	EntryPoints []string `json:"entryPoints"`
	Rule        string   `json:"rule"`
	Service     string   `json:"service"`
}

// TCPService is a single Traefik TCP service entry.
type TCPService struct {
	LoadBalancer TCPLoadBalancer `json:"loadBalancer"`
}

// TCPLoadBalancer holds the upstream server list for a Traefik TCP service.
type TCPLoadBalancer struct {
	Servers []TCPServer `json:"servers"`
}

// TCPServer is a single upstream server address (TCP).
type TCPServer struct {
	Address string `json:"address"`
}

// UDPConfig holds the routers and services for the Traefik UDP provider section.
type UDPConfig struct {
	Routers  map[string]UDPRouter  `json:"routers"`
	Services map[string]UDPService `json:"services"`
}

// UDPRouter is a single Traefik UDP router entry (no Rule field — UDP has none).
type UDPRouter struct {
	EntryPoints []string `json:"entryPoints"`
	Service     string   `json:"service"`
}

// UDPService is a single Traefik UDP service entry.
type UDPService struct {
	LoadBalancer UDPLoadBalancer `json:"loadBalancer"`
}

// UDPLoadBalancer holds the upstream server list for a Traefik UDP service.
type UDPLoadBalancer struct {
	Servers []UDPServer `json:"servers"`
}

// UDPServer is a single upstream server address (UDP).
type UDPServer struct {
	Address string `json:"address"`
}

var multiHyphen = regexp.MustCompile(`-{2,}`)

// sanitiseName converts an arbitrary string to a valid Traefik identifier:
// lowercase, non-alphanumeric chars replaced with hyphens, consecutive hyphens
// collapsed, leading/trailing hyphens trimmed.
func sanitiseName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(multiHyphen.ReplaceAllString(b.String(), "-"), "-")
}

// containsInt reports whether n is present in slice.
func containsInt(slice []int, n int) bool {
	for _, v := range slice {
		if v == n {
			return true
		}
	}
	return false
}

// BuildConfig builds a Traefik HTTP provider config from a slice of per-container
// snapshots. proxyHost is the hostname Traefik uses to reach lazy-tcp-proxy.
// entryPoint and certResolver are applied to every HTTP router when non-empty.
//
// HTTP section: one router+service per TraefikHosts entry whose port is in TCPPorts.
// TCP section:  one router+service per TCPPorts entry (HostSNI catch-all).
// UDP section:  one router+service per UDPPorts entry (no rule).
func BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver string) TraefikConfig {
	httpRouters := make(map[string]HTTPRouter)
	httpServices := make(map[string]HTTPService)
	tcpRouters := make(map[string]TCPRouter)
	tcpServices := make(map[string]TCPService)
	udpRouters := make(map[string]UDPRouter)
	udpServices := make(map[string]UDPService)

	for _, snap := range snapshots {
		prefix := sanitiseName(snap.ContainerName)

		// HTTP section — from explicit traefik_hosts entries.
		for _, entry := range snap.TraefikHosts {
			idx := strings.LastIndex(entry, ":")
			if idx < 1 {
				continue
			}
			domain := entry[:idx]
			port, err := strconv.Atoi(entry[idx+1:])
			if err != nil || !containsInt(snap.TCPPorts, port) {
				continue
			}

			name := sanitiseName(fmt.Sprintf("%s-%d", domain, port))
			routerName := name + "-router"
			serviceName := name + "-service"

			router := HTTPRouter{
				Rule:    fmt.Sprintf("Host(`%s`)", domain),
				Service: serviceName,
			}
			if entryPoint != "" {
				router.EntryPoints = []string{entryPoint}
			}
			if certResolver != "" {
				router.TLS = &RouterTLS{CertResolver: certResolver}
			}
			httpRouters[routerName] = router
			httpServices[serviceName] = HTTPService{
				LoadBalancer: LoadBalancer{
					Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, port)}},
				},
			}
		}

		// TCP section — one entry per TCP listen port.
		for _, port := range snap.TCPPorts {
			name := fmt.Sprintf("%s-tcp-%d", prefix, port)
			tcpRouters[name+"-router"] = TCPRouter{
				EntryPoints: []string{fmt.Sprintf("tcp-%d", port)},
				Rule:        "HostSNI(`*`)",
				Service:     name + "-service",
			}
			tcpServices[name+"-service"] = TCPService{
				LoadBalancer: TCPLoadBalancer{
					Servers: []TCPServer{{Address: fmt.Sprintf("%s:%d", proxyHost, port)}},
				},
			}
		}

		// UDP section — one entry per UDP listen port.
		for _, port := range snap.UDPPorts {
			name := fmt.Sprintf("%s-udp-%d", prefix, port)
			udpRouters[name+"-router"] = UDPRouter{
				EntryPoints: []string{fmt.Sprintf("udp-%d", port)},
				Service:     name + "-service",
			}
			udpServices[name+"-service"] = UDPService{
				LoadBalancer: UDPLoadBalancer{
					Servers: []UDPServer{{Address: fmt.Sprintf("%s:%d", proxyHost, port)}},
				},
			}
		}
	}

	cfg := TraefikConfig{HTTP: HTTPConfig{Routers: httpRouters, Services: httpServices}}
	if len(tcpRouters) > 0 {
		cfg.TCP = &TCPConfig{Routers: tcpRouters, Services: tcpServices}
	}
	if len(udpRouters) > 0 {
		cfg.UDP = &UDPConfig{Routers: udpRouters, Services: udpServices}
	}
	return cfg
}
