package traefik

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Snapshot is the minimal per-listen-port info needed to build the Traefik config.
// Defined here so this package does not import the proxy package.
type Snapshot struct {
	ListenPort      int
	TraefikHosts    []string // domain:port pairs for HTTP section, e.g. ["whoami.localhost:9001"]
	TraefikTCPHosts []string // domain:port pairs for TCP SNI section, e.g. ["mongo.example.com:27015"]
	ALPNProtocols   []string // extra TLS ALPN protocols for this target's routers, e.g. ["postgresql"]
	MinTLSVersion   string   // "", "1.0", "1.1", "1.2" or "1.3"
}

// TraefikConfig is the top-level Traefik HTTP provider dynamic config payload.
type TraefikConfig struct {
	HTTP HTTPConfig `json:"http,omitzero"`
	TCP  *TCPConfig `json:"tcp,omitempty"`
	TLS  *TLSConfig `json:"tls,omitempty"`
}

// TLSConfig holds the named TLS options referenced by routers via tls.options.
type TLSConfig struct {
	Options map[string]TLSOption `json:"options"`
}

// TLSOption is a single named Traefik TLS option.
type TLSOption struct {
	AlpnProtocols []string `json:"alpnProtocols,omitempty"`
	MinVersion    string   `json:"minVersion,omitempty"`
}

// HTTPConfig holds the routers and services sections of the HTTP provider payload.
type HTTPConfig struct {
	Routers           map[string]HTTPRouter           `json:"routers,omitempty"`
	Services          map[string]HTTPService          `json:"services,omitempty"`
	ServersTransports map[string]ServersTransport     `json:"serversTransports,omitempty"`
}

// IsZero reports whether the HTTP section has nothing to emit. It is used by the
// encoding/json omitzero option: Traefik rejects the whole payload ("routers cannot
// be a standalone element") when it receives empty http routers/services maps.
func (c HTTPConfig) IsZero() bool {
	return len(c.Routers) == 0 && len(c.Services) == 0 && len(c.ServersTransports) == 0
}

// ServersTransport holds timeout configuration for upstream connections.
type ServersTransport struct {
	ForwardingTimeouts ForwardingTimeouts `json:"forwardingTimeouts"`
}

// ForwardingTimeouts holds the individual timeout values for a ServersTransport.
type ForwardingTimeouts struct {
	DialTimeout           string `json:"dialTimeout,omitempty"`
	ResponseHeaderTimeout string `json:"responseHeaderTimeout,omitempty"`
	IdleConnTimeout       string `json:"idleConnTimeout,omitempty"`
}

// RouterTLS holds the TLS options for a Traefik router.
type RouterTLS struct {
	CertResolver string `json:"certResolver,omitempty"`
	Options      string `json:"options,omitempty"`
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
	Servers          []Server `json:"servers"`
	ServersTransport string   `json:"serversTransport,omitempty"`
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

// traefikDefaultALPN is the protocol list Traefik advertises when no alpnProtocols
// are configured. Setting alpnProtocols replaces it, so it is re-emitted in full.
var traefikDefaultALPN = []string{"h2", "http/1.1", "acme-tls/1"}

var minVersionNames = map[string]string{
	"1.0": "VersionTLS10",
	"1.1": "VersionTLS11",
	"1.2": "VersionTLS12",
	"1.3": "VersionTLS13",
}

// tlsOptionFor builds the named TLS option for a snapshot's ALPN protocols and
// minimum TLS version. ok is false when the snapshot needs no custom option.
func tlsOptionFor(snap Snapshot) (name string, opt TLSOption, ok bool) {
	var extras []string
	for _, p := range snap.ALPNProtocols {
		if p == "" || slices.Contains(traefikDefaultALPN, p) || slices.Contains(extras, p) {
			continue
		}
		extras = append(extras, p)
	}
	name = "lazy-tls"
	if len(extras) > 0 {
		opt.AlpnProtocols = append(append([]string{}, traefikDefaultALPN...), extras...)
		name += "-alpn-" + sanitiseName(strings.Join(extras, "-"))
	}
	if v, found := minVersionNames[snap.MinTLSVersion]; found {
		opt.MinVersion = v
		name += "-min-" + sanitiseName(snap.MinTLSVersion)
	}
	return name, opt, len(extras) > 0 || opt.MinVersion != ""
}

// uniqueOptionName returns name if it is free or already holds an identical option;
// otherwise it appends a numeric suffix. This guards against two distinct option
// sets sanitising to the same identifier (e.g. "x/y" and "x-y").
func uniqueOptionName(existing map[string]TLSOption, name string, opt TLSOption) string {
	candidate := name
	for i := 2; ; i++ {
		cur, taken := existing[candidate]
		if !taken || (slices.Equal(cur.AlpnProtocols, opt.AlpnProtocols) && cur.MinVersion == opt.MinVersion) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
}

// routerEntryPoints returns the entryPoints slice for a generated router, or nil
// when no entry point is configured.
func routerEntryPoints(entryPoint string) []string {
	if entryPoint == "" {
		return nil
	}
	return []string{entryPoint}
}

// routerTLS returns the tls block for a generated router, or nil when neither a
// cert resolver nor a TLS option is configured.
func routerTLS(certResolver, options string) *RouterTLS {
	if certResolver == "" && options == "" {
		return nil
	}
	return &RouterTLS{CertResolver: certResolver, Options: options}
}

// TransportTimeouts holds optional timeout overrides emitted as a named ServersTransport.
// Empty strings mean the field is omitted (Traefik uses its own default).
type TransportTimeouts struct {
	DialTimeout           string
	ResponseHeaderTimeout string
	IdleConnTimeout       string
}

// BuildConfig builds a Traefik HTTP provider config from a slice of per-listen-port
// snapshots. proxyHost is the hostname Traefik uses to reach lazy-tcp-proxy.
// entryPoint and certResolver are applied to every emitted router when non-empty.
// When webHost is non-empty, an additional HTTP router+service is emitted that routes
// Host(`webHost`) → http://proxyHost:webPort (exposing the lazy-tcp-proxy web endpoint).
// When timeouts is non-nil and has at least one field set, a named ServersTransport
// ("lazy-transport") is emitted and wired to every HTTP service.
//
// HTTP section: one router+service per TraefikHosts entry whose port matches ListenPort.
// TCP section:  one router+service per TraefikTCPHosts entry whose port matches ListenPort,
//
//	using HostSNI rule on the configured entrypoint.
func BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver, webHost string, webPort int, timeouts *TransportTimeouts) TraefikConfig {
	const transportName = "lazy-transport"
	useTransport := timeouts != nil && (timeouts.DialTimeout != "" || timeouts.ResponseHeaderTimeout != "" || timeouts.IdleConnTimeout != "")

	httpRouters := make(map[string]HTTPRouter)
	httpServices := make(map[string]HTTPService)
	tcpRouters := make(map[string]TCPRouter)
	tcpServices := make(map[string]TCPService)

	tlsOptions := make(map[string]TLSOption)

	for _, snap := range snapshots {
		// Per-service TLS option, referenced only by this snapshot's routers.
		var optName string
		if name, opt, ok := tlsOptionFor(snap); ok {
			optName = uniqueOptionName(tlsOptions, name, opt)
			tlsOptions[optName] = opt
		}

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
			router.EntryPoints = routerEntryPoints(entryPoint)
			router.TLS = routerTLS(certResolver, optName)
			httpRouters[name+"-router"] = router
			lb := LoadBalancer{Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, port)}}}
			if useTransport {
				lb.ServersTransport = transportName
			}
			httpServices[name+"-service"] = HTTPService{LoadBalancer: lb}
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
			router.EntryPoints = routerEntryPoints(entryPoint)
			router.TLS = routerTLS(certResolver, optName)
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
		router.EntryPoints = routerEntryPoints(entryPoint)
		router.TLS = routerTLS(certResolver, "")
		httpRouters[name+"-router"] = router
		lb := LoadBalancer{Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, webPort)}}}
		if useTransport {
			lb.ServersTransport = transportName
		}
		httpServices[name+"-service"] = HTTPService{LoadBalancer: lb}
	}

	httpCfg := HTTPConfig{Routers: httpRouters, Services: httpServices}
	if useTransport {
		httpCfg.ServersTransports = map[string]ServersTransport{
			transportName: {
				ForwardingTimeouts: ForwardingTimeouts{
					DialTimeout:           timeouts.DialTimeout,
					ResponseHeaderTimeout: timeouts.ResponseHeaderTimeout,
					IdleConnTimeout:       timeouts.IdleConnTimeout,
				},
			},
		}
	}
	cfg := TraefikConfig{HTTP: httpCfg}
	if len(tcpRouters) > 0 {
		cfg.TCP = &TCPConfig{Routers: tcpRouters, Services: tcpServices}
	}
	if len(tlsOptions) > 0 {
		cfg.TLS = &TLSConfig{Options: tlsOptions}
	}
	return cfg
}
