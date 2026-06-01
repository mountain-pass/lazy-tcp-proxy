package traefik

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Snapshot is the minimal per-listen-port info needed to build the Traefik config.
// Defined here so this package does not import the proxy package.
type Snapshot struct {
	ListenPort      int
	TraefikHosts    []string // domain:port pairs for HTTP section, e.g. ["whoami.localhost:9001"]
	TraefikTCPHosts []string // domain:port pairs for TCP SNI section, e.g. ["mongo.example.com:27015"]
}

// TraefikConfig is the top-level Traefik HTTP provider dynamic config payload.
type TraefikConfig struct {
	HTTP HTTPConfig `json:"http"`
	TCP  *TCPConfig `json:"tcp,omitempty"`
}

// HTTPConfig holds the routers and services sections of the HTTP provider payload.
type HTTPConfig struct {
	Routers  map[string]HTTPRouter  `json:"routers"`
	Services map[string]HTTPService `json:"services"`
}

// RouterTLS holds the TLS options for a Traefik router.
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
	EntryPoints []string   `json:"entryPoints,omitempty"`
	Rule        string     `json:"rule"`
	Service     string     `json:"service"`
	TLS         *RouterTLS `json:"tls,omitempty"`
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

// BuildConfig builds a Traefik HTTP provider config from a slice of per-listen-port
// snapshots. proxyHost is the hostname Traefik uses to reach lazy-tcp-proxy.
// entryPoint and certResolver are applied to every emitted router when non-empty.
// When webHost is non-empty, an additional HTTP router+service is emitted that routes
// Host(`webHost`) → http://proxyHost:webPort (exposing the lazy-tcp-proxy web endpoint).
//
// HTTP section: one router+service per TraefikHosts entry whose port matches ListenPort.
// TCP section:  one router+service per TraefikTCPHosts entry whose port matches ListenPort,
//
//	using HostSNI rule on the configured entrypoint.
func BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver, webHost string, webPort int) TraefikConfig {
	httpRouters := make(map[string]HTTPRouter)
	httpServices := make(map[string]HTTPService)
	tcpRouters := make(map[string]TCPRouter)
	tcpServices := make(map[string]TCPService)

	for _, snap := range snapshots {
		// HTTP section — from explicit traefik_hosts entries.
		for _, entry := range snap.TraefikHosts {
			idx := strings.LastIndex(entry, ":")
			if idx < 1 {
				continue
			}
			domain := entry[:idx]
			port, err := strconv.Atoi(entry[idx+1:])
			if err != nil || port != snap.ListenPort {
				continue
			}

			name := sanitiseName(fmt.Sprintf("%s-%d", domain, port))
			router := HTTPRouter{
				Rule:    fmt.Sprintf("Host(`%s`)", domain),
				Service: name + "-service",
			}
			if entryPoint != "" {
				router.EntryPoints = []string{entryPoint}
			}
			if certResolver != "" {
				router.TLS = &RouterTLS{CertResolver: certResolver}
			}
			httpRouters[name+"-router"] = router
			httpServices[name+"-service"] = HTTPService{
				LoadBalancer: LoadBalancer{
					Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, port)}},
				},
			}
		}

		// TCP section — from explicit traefik_tcp_hosts entries (HostSNI routing).
		for _, entry := range snap.TraefikTCPHosts {
			idx := strings.LastIndex(entry, ":")
			if idx < 1 {
				continue
			}
			domain := entry[:idx]
			port, err := strconv.Atoi(entry[idx+1:])
			if err != nil || port != snap.ListenPort {
				continue
			}

			name := sanitiseName(fmt.Sprintf("%s-%d", domain, port))
			router := TCPRouter{
				Rule:    fmt.Sprintf("HostSNI(`%s`)", domain),
				Service: name + "-service",
			}
			if entryPoint != "" {
				router.EntryPoints = []string{entryPoint}
			}
			if certResolver != "" {
				router.TLS = &RouterTLS{CertResolver: certResolver}
			}
			tcpRouters[name+"-router"] = router
			tcpServices[name+"-service"] = TCPService{
				LoadBalancer: TCPLoadBalancer{
					Servers: []TCPServer{{Address: fmt.Sprintf("%s:%d", proxyHost, port)}},
				},
			}
		}
	}

	if webHost != "" {
		name := sanitiseName(fmt.Sprintf("%s-%d", webHost, webPort))
		router := HTTPRouter{
			Rule:    fmt.Sprintf("Host(`%s`)", webHost),
			Service: name + "-service",
		}
		if entryPoint != "" {
			router.EntryPoints = []string{entryPoint}
		}
		if certResolver != "" {
			router.TLS = &RouterTLS{CertResolver: certResolver}
		}
		httpRouters[name+"-router"] = router
		httpServices[name+"-service"] = HTTPService{
			LoadBalancer: LoadBalancer{
				Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, webPort)}},
			},
		}
	}

	cfg := TraefikConfig{HTTP: HTTPConfig{Routers: httpRouters, Services: httpServices}}
	if len(tcpRouters) > 0 {
		cfg.TCP = &TCPConfig{Routers: tcpRouters, Services: tcpServices}
	}
	return cfg
}
