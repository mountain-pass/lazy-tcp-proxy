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
	ListenPort   int
	TraefikHosts []string // e.g. ["whoami.localhost:9001"]
}

// TraefikConfig is the top-level Traefik HTTP provider dynamic config payload.
type TraefikConfig struct {
	HTTP HTTPConfig `json:"http"`
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

// LoadBalancer holds the upstream server list for a Traefik service.
type LoadBalancer struct {
	Servers []Server `json:"servers"`
}

// Server is a single upstream server URL.
type Server struct {
	URL string `json:"url"`
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

// BuildConfig builds a Traefik HTTP provider config from a slice of proxy
// snapshots. For each traefik_hosts entry whose port matches the snapshot's
// ListenPort, one router+service pair is emitted. proxyHost is the hostname
// Traefik uses to reach lazy-tcp-proxy (e.g. "lazy-tcp-proxy"). entryPoint
// and certResolver are applied to every emitted router when non-empty.
func BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver string) TraefikConfig {
	routers := make(map[string]HTTPRouter)
	services := make(map[string]HTTPService)

	for _, snap := range snapshots {
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
			routers[routerName] = router
			services[serviceName] = HTTPService{
				LoadBalancer: LoadBalancer{
					Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, port)}},
				},
			}
		}
	}

	return TraefikConfig{HTTP: HTTPConfig{Routers: routers, Services: services}}
}
