package traefik

import (
	"encoding/json"
	"testing"
)

func TestBuildConfig_SingleHost(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "whoami", TCPPorts: []int{9001}, TraefikHosts: []string{"whoami.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

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
	// traefik-hosts entry port (9002) not in TCPPorts ([9001]) — should be skipped
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"whoami.localhost:9002"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if len(cfg.HTTP.Routers) != 0 {
		t.Errorf("expected 0 routers, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_MultipleHostsSamePort(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"app-a.localhost:9001", "app-b.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if len(cfg.HTTP.Routers) != 2 {
		t.Errorf("expected 2 routers, got %d", len(cfg.HTTP.Routers))
	}
	if len(cfg.HTTP.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg.HTTP.Services))
	}
}

func TestBuildConfig_MultipleSnapshots(t *testing.T) {
	snaps := []Snapshot{
		{ContainerName: "app1", TCPPorts: []int{9001}, TraefikHosts: []string{"app1.localhost:9001"}},
		{ContainerName: "app2", TCPPorts: []int{9002}, TraefikHosts: []string{"app2.localhost:9002"}},
	}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if len(cfg.HTTP.Routers) != 2 {
		t.Errorf("expected 2 routers, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_NoHosts(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: nil}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if len(cfg.HTTP.Routers) != 0 || len(cfg.HTTP.Services) != 0 {
		t.Errorf("expected empty HTTP config, got %+v", cfg.HTTP)
	}
}

func TestBuildConfig_EmptySnapshots(t *testing.T) {
	cfg := BuildConfig(nil, "lazy-tcp-proxy", "", "")

	if len(cfg.HTTP.Routers) != 0 || len(cfg.HTTP.Services) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestBuildConfig_CustomProxyHost(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "10.0.0.5", "", "")

	svc := cfg.HTTP.Services["app-localhost-9001-service"]
	if svc.LoadBalancer.Servers[0].URL != "http://10.0.0.5:9001" {
		t.Errorf("unexpected URL: %s", svc.LoadBalancer.Servers[0].URL)
	}
}

func TestBuildConfig_MalformedEntrySkipped(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"noport", "bad:xyz", "ok.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if len(cfg.HTTP.Routers) != 1 {
		t.Errorf("expected 1 router, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_WithEntryPoint(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "")

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints=[websecure], got %v", r.EntryPoints)
	}
	if r.TLS != nil {
		t.Errorf("expected no tls, got %+v", r.TLS)
	}
}

func TestBuildConfig_WithCertResolver(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "myresolver")

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if r.EntryPoints != nil {
		t.Errorf("expected no entryPoints, got %v", r.EntryPoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "myresolver" {
		t.Errorf("expected tls.certResolver=myresolver, got %+v", r.TLS)
	}
}

func TestBuildConfig_WithBoth(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "websecure", "myresolver")

	r := cfg.HTTP.Routers["app-localhost-9001-router"]
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints=[websecure], got %v", r.EntryPoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "myresolver" {
		t.Errorf("expected tls.certResolver=myresolver, got %+v", r.TLS)
	}
}

func TestBuildConfig_NeitherSet(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

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

// --- TCP section tests ---

func TestBuildConfig_TCPSection_Single(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "postgres", TCPPorts: []int{5432}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.TCP == nil {
		t.Fatal("expected tcp section, got nil")
	}
	r, ok := cfg.TCP.Routers["postgres-tcp-5432-router"]
	if !ok {
		t.Fatal("router 'postgres-tcp-5432-router' not found")
	}
	if r.Rule != "HostSNI(`*`)" {
		t.Errorf("unexpected rule: %s", r.Rule)
	}
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "tcp-5432" {
		t.Errorf("unexpected entryPoints: %v", r.EntryPoints)
	}
	if r.Service != "postgres-tcp-5432-service" {
		t.Errorf("unexpected service: %s", r.Service)
	}
	svc, ok := cfg.TCP.Services["postgres-tcp-5432-service"]
	if !ok {
		t.Fatal("service 'postgres-tcp-5432-service' not found")
	}
	if len(svc.LoadBalancer.Servers) != 1 || svc.LoadBalancer.Servers[0].Address != "lazy-tcp-proxy:5432" {
		t.Errorf("unexpected address: %+v", svc.LoadBalancer.Servers)
	}
}

func TestBuildConfig_TCPSection_Multiple(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{5432, 5433}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.TCP == nil {
		t.Fatal("expected tcp section")
	}
	if len(cfg.TCP.Routers) != 2 {
		t.Errorf("expected 2 TCP routers, got %d", len(cfg.TCP.Routers))
	}
	if len(cfg.TCP.Services) != 2 {
		t.Errorf("expected 2 TCP services, got %d", len(cfg.TCP.Services))
	}
}

func TestBuildConfig_TCPSection_NoTCPPorts(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: nil}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.TCP != nil {
		t.Errorf("expected tcp section absent, got %+v", cfg.TCP)
	}
	// Verify JSON omits the key entirely.
	data, _ := json.Marshal(cfg)
	if contains(data, `"tcp"`) {
		t.Errorf("expected 'tcp' key absent from JSON, got: %s", data)
	}
}

func TestBuildConfig_TCPSection_CustomProxyHost(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{5432}}}
	cfg := BuildConfig(snaps, "my-host", "", "")

	svc := cfg.TCP.Services["app-tcp-5432-service"]
	if svc.LoadBalancer.Servers[0].Address != "my-host:5432" {
		t.Errorf("unexpected address: %s", svc.LoadBalancer.Servers[0].Address)
	}
}

// --- UDP section tests ---

func TestBuildConfig_UDPSection_Single(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "dns", UDPPorts: []int{53}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.UDP == nil {
		t.Fatal("expected udp section, got nil")
	}
	r, ok := cfg.UDP.Routers["dns-udp-53-router"]
	if !ok {
		t.Fatal("router 'dns-udp-53-router' not found")
	}
	if len(r.EntryPoints) != 1 || r.EntryPoints[0] != "udp-53" {
		t.Errorf("unexpected entryPoints: %v", r.EntryPoints)
	}
	if r.Service != "dns-udp-53-service" {
		t.Errorf("unexpected service: %s", r.Service)
	}
	svc, ok := cfg.UDP.Services["dns-udp-53-service"]
	if !ok {
		t.Fatal("service 'dns-udp-53-service' not found")
	}
	if len(svc.LoadBalancer.Servers) != 1 || svc.LoadBalancer.Servers[0].Address != "lazy-tcp-proxy:53" {
		t.Errorf("unexpected address: %+v", svc.LoadBalancer.Servers)
	}
}

func TestBuildConfig_UDPSection_Multiple(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "dns", UDPPorts: []int{53, 5353}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.UDP == nil {
		t.Fatal("expected udp section")
	}
	if len(cfg.UDP.Routers) != 2 {
		t.Errorf("expected 2 UDP routers, got %d", len(cfg.UDP.Routers))
	}
}

func TestBuildConfig_UDPSection_NoUDPPorts(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "app", TCPPorts: []int{9001}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.UDP != nil {
		t.Errorf("expected udp section absent, got %+v", cfg.UDP)
	}
	data, _ := json.Marshal(cfg)
	if contains(data, `"udp"`) {
		t.Errorf("expected 'udp' key absent from JSON, got: %s", data)
	}
}

func TestBuildConfig_UDPSection_NoRuleField(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "dns", UDPPorts: []int{53}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	data, _ := json.Marshal(cfg.UDP)
	if contains(data, `"rule"`) {
		t.Errorf("UDP section must not contain 'rule' field, got: %s", data)
	}
}

func TestBuildConfig_TCPAndUDP_SamePort(t *testing.T) {
	snaps := []Snapshot{{ContainerName: "dns", TCPPorts: []int{53}, UDPPorts: []int{53}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy", "", "")

	if cfg.TCP == nil {
		t.Fatal("expected tcp section")
	}
	if cfg.UDP == nil {
		t.Fatal("expected udp section")
	}
	if _, ok := cfg.TCP.Routers["dns-tcp-53-router"]; !ok {
		t.Error("tcp router 'dns-tcp-53-router' not found")
	}
	if _, ok := cfg.UDP.Routers["dns-udp-53-router"]; !ok {
		t.Error("udp router 'dns-udp-53-router' not found")
	}
	tcpAddr := cfg.TCP.Services["dns-tcp-53-service"].LoadBalancer.Servers[0].Address
	udpAddr := cfg.UDP.Services["dns-udp-53-service"].LoadBalancer.Servers[0].Address
	if tcpAddr != "lazy-tcp-proxy:53" {
		t.Errorf("unexpected tcp address: %s", tcpAddr)
	}
	if udpAddr != "lazy-tcp-proxy:53" {
		t.Errorf("unexpected udp address: %s", udpAddr)
	}
}

func contains(data []byte, s string) bool {
	return len(data) > 0 && string(data) != "" && len(s) > 0 &&
		(func() bool {
			for i := 0; i <= len(data)-len(s); i++ {
				if string(data[i:i+len(s)]) == s {
					return true
				}
			}
			return false
		})()
}
