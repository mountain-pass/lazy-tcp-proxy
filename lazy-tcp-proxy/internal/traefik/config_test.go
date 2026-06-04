package traefik

import (
	"encoding/json"
	"testing"
)

func TestBuildConfig_SingleHost(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"whoami.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(cfg.HTTP.Routers))
	}
	r, ok := cfg.HTTP.Routers["whoami-localhost-9001-router"]
	if !ok {
		t.Fatal("router 'whoami-localhost-9001-router' not found")
	}
	if r.Rule != "Host(`whoami.localhost`)" {
		t.Errorf("unexpected rule: %s", r.Rule)
	}
	if r.Service != "whoami-localhost-9001-service" {
		t.Errorf("unexpected service name: %s", r.Service)
	}

	svc, ok := cfg.HTTP.Services["whoami-localhost-9001-service"]
	if !ok {
		t.Fatal("service 'whoami-localhost-9001-service' not found")
	}
	if len(svc.LoadBalancer.Servers) != 1 || svc.LoadBalancer.Servers[0].URL != "http://lazy-tcp-proxy:9001" {
		t.Errorf("unexpected server URL: %+v", svc.LoadBalancer.Servers)
	}
}

func TestBuildConfig_PortMismatch(t *testing.T) {
	// entry port (9002) does not match snapshot ListenPort (9001) — should be skipped
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"whoami.localhost:9002"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 0 {
		t.Errorf("expected 0 routers, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_MultipleHostsSamePort(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app-a.localhost:9001", "app-b.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 2 {
		t.Errorf("expected 2 routers, got %d", len(cfg.HTTP.Routers))
	}
	if len(cfg.HTTP.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg.HTTP.Services))
	}
}

func TestBuildConfig_MultipleSnapshots(t *testing.T) {
	snaps := []Snapshot{
		{ListenPort: 9001, TraefikHosts: []string{"app1.localhost:9001"}},
		{ListenPort: 9002, TraefikHosts: []string{"app2.localhost:9002"}},
	}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 2 {
		t.Errorf("expected 2 routers, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_NoHosts(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: nil}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 0 || len(cfg.HTTP.Services) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestBuildConfig_EmptySnapshots(t *testing.T) {
	cfg := BuildConfig(nil, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 0 || len(cfg.HTTP.Services) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestBuildConfig_CustomProxyHost(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "10.0.0.5", "", "", "", 0, nil)

	svc := cfg.HTTP.Services["app-localhost-9001-service"]
	if svc.LoadBalancer.Servers[0].URL != "http://10.0.0.5:9001" {
		t.Errorf("unexpected URL: %s", svc.LoadBalancer.Servers[0].URL)
	}
}

func TestBuildConfig_MalformedEntrySkipped(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"noport", "bad:xyz", "ok.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	// Only the valid entry should produce output
	if len(cfg.HTTP.Routers) != 1 {
		t.Errorf("expected 1 router, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_WithEntryPoint(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "", "", 0, nil)

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints=[websecure], got %v", r.EntryPoints)
	}
	if r.TLS != nil {
		t.Errorf("expected no tls, got %+v", r.TLS)
	}
}

func TestBuildConfig_WithCertResolver(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "myresolver", "", 0, nil)

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if r.EntryPoints != nil {
		t.Errorf("expected no entryPoints, got %v", r.EntryPoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "myresolver" {
		t.Errorf("expected tls.certResolver=myresolver, got %+v", r.TLS)
	}
}

func TestBuildConfig_WithBoth(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "myresolver", "", 0, nil)

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints=[websecure], got %v", r.EntryPoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "myresolver" {
		t.Errorf("expected tls.certResolver=myresolver, got %+v", r.TLS)
	}
}

func TestBuildConfig_NeitherSet(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if r.EntryPoints != nil {
		t.Errorf("expected no entryPoints, got %v", r.EntryPoints)
	}
	if r.TLS != nil {
		t.Errorf("expected no tls, got %+v", r.TLS)
	}
}

func TestSanitiseName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"whoami.localhost-9001", "whoami-localhost-9001"},
		{"my_app.localhost-9001", "my-app-localhost-9001"},
		{"MY.APP-9001", "my-app-9001"},
		{"--leading--trailing--", "leading-trailing"},
		{"a..b", "a-b"},
	}
	for _, c := range cases {
		got := sanitiseName(c.in)
		if got != c.want {
			t.Errorf("sanitiseName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- TCP SNI section tests ---

func TestBuildConfig_TCPSection_Single(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 27015, TraefikTCPHosts: []string{"mongo.example.com:27015"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "myresolver", "", 0, nil)

	if cfg.TCP == nil {
		t.Fatal("expected tcp section, got nil")
	}
	r, ok := cfg.TCP.Routers["mongo-example-com-27015-router"]
	if !ok {
		t.Fatal("router 'mongo-example-com-27015-router' not found")
	}
	if r.Rule != "HostSNI(`mongo.example.com`)" {
		t.Errorf("unexpected rule: %s", r.Rule)
	}
	if r.Service != "mongo-example-com-27015-service" {
		t.Errorf("unexpected service: %s", r.Service)
	}
	svc, ok := cfg.TCP.Services["mongo-example-com-27015-service"]
	if !ok {
		t.Fatal("service 'mongo-example-com-27015-service' not found")
	}
	if len(svc.LoadBalancer.Servers) != 1 || svc.LoadBalancer.Servers[0].Address != "lazy-tcp-proxy:27015" {
		t.Errorf("unexpected address: %+v", svc.LoadBalancer.Servers)
	}
}

func TestBuildConfig_TCPSection_HostSNIRule(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 27015, TraefikTCPHosts: []string{"mongo.example.com:27015"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	r := cfg.TCP.Routers["mongo-example-com-27015-router"]
	if r.Rule != "HostSNI(`mongo.example.com`)" {
		t.Errorf("expected HostSNI rule, got: %s", r.Rule)
	}
}

func TestBuildConfig_TCPSection_UsesAddress(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 27015, TraefikTCPHosts: []string{"mongo.example.com:27015"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	svc := cfg.TCP.Services["mongo-example-com-27015-service"]
	data, _ := json.Marshal(svc)
	if !jsonContains(data, `"address"`) {
		t.Errorf("expected 'address' field in TCP service, got: %s", data)
	}
	if jsonContains(data, `"url"`) {
		t.Errorf("TCP service must not use 'url' field, got: %s", data)
	}
}

func TestBuildConfig_TCPSection_PortMismatch(t *testing.T) {
	// entry port (9999) does not match ListenPort (27015) — should be skipped
	snaps := []Snapshot{{ListenPort: 27015, TraefikTCPHosts: []string{"mongo.example.com:9999"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if cfg.TCP != nil {
		t.Errorf("expected tcp section absent, got %+v", cfg.TCP)
	}
}

func TestBuildConfig_TCPSection_NoEntries(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikTCPHosts: nil}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if cfg.TCP != nil {
		t.Errorf("expected tcp section absent, got %+v", cfg.TCP)
	}
	data, _ := json.Marshal(cfg)
	if jsonContains(data, `"tcp"`) {
		t.Errorf("expected 'tcp' key absent from JSON, got: %s", data)
	}
}

func TestBuildConfig_TCPSection_WithCertResolver(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 27015, TraefikTCPHosts: []string{"mongo.example.com:27015"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "myresolver", "", 0, nil)

	r := cfg.TCP.Routers["mongo-example-com-27015-router"]
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints=[websecure], got %v", r.EntryPoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "myresolver" {
		t.Errorf("expected tls.certResolver=myresolver, got %+v", r.TLS)
	}
}

func TestBuildConfig_TCPAndHTTP_SameDomain(t *testing.T) {
	// same domain in both traefik_hosts (HTTP) and traefik_tcp_hosts (TCP) — no collision
	snaps := []Snapshot{{
		ListenPort:      9001,
		TraefikHosts:    []string{"app.example.com:9001"},
		TraefikTCPHosts: []string{"app.example.com:9001"},
	}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "myresolver", "", 0, nil)

	if _, ok := cfg.HTTP.Routers["app-example-com-9001-router"]; !ok {
		t.Error("HTTP router 'app-example-com-9001-router' not found")
	}
	if cfg.TCP == nil {
		t.Fatal("expected tcp section")
	}
	if _, ok := cfg.TCP.Routers["app-example-com-9001-router"]; !ok {
		t.Error("TCP router 'app-example-com-9001-router' not found")
	}
	httpSvc := cfg.HTTP.Services["app-example-com-9001-service"]
	if len(httpSvc.LoadBalancer.Servers) != 1 || httpSvc.LoadBalancer.Servers[0].URL == "" {
		t.Errorf("HTTP service should have url, got %+v", httpSvc.LoadBalancer.Servers)
	}
	tcpSvc := cfg.TCP.Services["app-example-com-9001-service"]
	if len(tcpSvc.LoadBalancer.Servers) != 1 || tcpSvc.LoadBalancer.Servers[0].Address == "" {
		t.Errorf("TCP service should have address, got %+v", tcpSvc.LoadBalancer.Servers)
	}
}

func TestBuildConfig_TCPSection_CustomProxyHost(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 27015, TraefikTCPHosts: []string{"mongo.example.com:27015"}}}
	cfg := BuildConfig(snaps, "my-host", "", "", "", 0, nil)

	svc := cfg.TCP.Services["mongo-example-com-27015-service"]
	if svc.LoadBalancer.Servers[0].Address != "my-host:27015" {
		t.Errorf("unexpected address: %s", svc.LoadBalancer.Servers[0].Address)
	}
}

// --- WEB_HOST route tests ---

func TestBuildConfig_WebHost(t *testing.T) {
	cfg := BuildConfig(nil, "lazy-tcp-proxy", "", "", "admin.foobar.com", 9090, nil)

	r, ok := cfg.HTTP.Routers["admin-foobar-com-9090-router"]
	if !ok {
		t.Fatal("router 'admin-foobar-com-9090-router' not found")
	}
	if r.Rule != "Host(`admin.foobar.com`)" {
		t.Errorf("unexpected rule: %s", r.Rule)
	}
	if r.Service != "admin-foobar-com-9090-service" {
		t.Errorf("unexpected service name: %s", r.Service)
	}
	svc, ok := cfg.HTTP.Services["admin-foobar-com-9090-service"]
	if !ok {
		t.Fatal("service 'admin-foobar-com-9090-service' not found")
	}
	if len(svc.LoadBalancer.Servers) != 1 || svc.LoadBalancer.Servers[0].URL != "http://lazy-tcp-proxy:9090" {
		t.Errorf("unexpected server URL: %+v", svc.LoadBalancer.Servers)
	}
}

func TestBuildConfig_WebHostEmpty(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"whoami.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if len(cfg.HTTP.Routers) != 1 {
		t.Errorf("expected exactly 1 router (no web host route), got %d", len(cfg.HTTP.Routers))
	}
	if _, ok := cfg.HTTP.Routers["whoami-localhost-9001-router"]; !ok {
		t.Error("expected whoami router to be present")
	}
}

func TestBuildConfig_WebHostWithEntryPointAndCertResolver(t *testing.T) {
	cfg := BuildConfig(nil, "lazy-tcp-proxy", "websecure", "myresolver", "admin.foobar.com", 8080, nil)

	r, ok := cfg.HTTP.Routers["admin-foobar-com-8080-router"]
	if !ok {
		t.Fatal("router 'admin-foobar-com-8080-router' not found")
	}
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints=[websecure], got %v", r.EntryPoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "myresolver" {
		t.Errorf("expected tls.certResolver=myresolver, got %+v", r.TLS)
	}
	svc := cfg.HTTP.Services["admin-foobar-com-8080-service"]
	if svc.LoadBalancer.Servers[0].URL != "http://lazy-tcp-proxy:8080" {
		t.Errorf("unexpected URL: %s", svc.LoadBalancer.Servers[0].URL)
	}
}

// --- ServersTransport tests ---

func TestBuildConfig_ServersTransport_NilTimeouts(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, nil)

	if cfg.HTTP.ServersTransports != nil {
		t.Errorf("expected no serversTransports, got %+v", cfg.HTTP.ServersTransports)
	}
	svc := cfg.HTTP.Services["app-localhost-9001-service"]
	if svc.LoadBalancer.ServersTransport != "" {
		t.Errorf("expected no serversTransport on service, got %q", svc.LoadBalancer.ServersTransport)
	}
}

func TestBuildConfig_ServersTransport_WithTimeouts(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	timeouts := &TransportTimeouts{
		DialTimeout:           "30s",
		ResponseHeaderTimeout: "15m",
		IdleConnTimeout:       "90s",
	}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, timeouts)

	if len(cfg.HTTP.ServersTransports) != 1 {
		t.Fatalf("expected 1 serversTransport, got %d", len(cfg.HTTP.ServersTransports))
	}
	tr, ok := cfg.HTTP.ServersTransports["lazy-transport"]
	if !ok {
		t.Fatal("expected 'lazy-transport' key in serversTransports")
	}
	if tr.ForwardingTimeouts.DialTimeout != "30s" {
		t.Errorf("unexpected dialTimeout: %s", tr.ForwardingTimeouts.DialTimeout)
	}
	if tr.ForwardingTimeouts.ResponseHeaderTimeout != "15m" {
		t.Errorf("unexpected responseHeaderTimeout: %s", tr.ForwardingTimeouts.ResponseHeaderTimeout)
	}
	if tr.ForwardingTimeouts.IdleConnTimeout != "90s" {
		t.Errorf("unexpected idleConnTimeout: %s", tr.ForwardingTimeouts.IdleConnTimeout)
	}

	svc := cfg.HTTP.Services["app-localhost-9001-service"]
	if svc.LoadBalancer.ServersTransport != "lazy-transport" {
		t.Errorf("expected service to reference lazy-transport, got %q", svc.LoadBalancer.ServersTransport)
	}
}

func TestBuildConfig_ServersTransport_EmptyTimeouts(t *testing.T) {
	// All empty strings → treated as no-op (useTransport=false)
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "", "", 0, &TransportTimeouts{})

	if cfg.HTTP.ServersTransports != nil {
		t.Errorf("expected no serversTransports for empty timeouts, got %+v", cfg.HTTP.ServersTransports)
	}
}

func jsonContains(data []byte, s string) bool {
	bs := []byte(s)
	if len(bs) > len(data) {
		return false
	}
	for i := 0; i <= len(data)-len(bs); i++ {
		match := true
		for j := range bs {
			if data[i+j] != bs[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
