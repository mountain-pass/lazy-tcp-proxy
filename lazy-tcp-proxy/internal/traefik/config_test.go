package traefik

import (
	"testing"
)

func TestBuildConfig_SingleHost(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"whoami.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy")

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
	cfg := BuildConfig(snaps, "lazy-tcp-proxy")

	if len(cfg.HTTP.Routers) != 0 {
		t.Errorf("expected 0 routers, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_MultipleHostsSamePort(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app-a.localhost:9001", "app-b.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy")

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
	cfg := BuildConfig(snaps, "lazy-tcp-proxy")

	if len(cfg.HTTP.Routers) != 2 {
		t.Errorf("expected 2 routers, got %d", len(cfg.HTTP.Routers))
	}
}

func TestBuildConfig_NoHosts(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: nil}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy")

	if len(cfg.HTTP.Routers) != 0 || len(cfg.HTTP.Services) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestBuildConfig_EmptySnapshots(t *testing.T) {
	cfg := BuildConfig(nil, "lazy-tcp-proxy")

	if len(cfg.HTTP.Routers) != 0 || len(cfg.HTTP.Services) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestBuildConfig_CustomProxyHost(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"app.localhost:9001"}}}
	cfg := BuildConfig(snaps, "10.0.0.5")

	svc := cfg.HTTP.Services["app-localhost-9001-service"]
	if svc.LoadBalancer.Servers[0].URL != "http://10.0.0.5:9001" {
		t.Errorf("unexpected URL: %s", svc.LoadBalancer.Servers[0].URL)
	}
}

func TestBuildConfig_MalformedEntrySkipped(t *testing.T) {
	snaps := []Snapshot{{ListenPort: 9001, TraefikHosts: []string{"noport", "bad:xyz", "ok.localhost:9001"}}}
	cfg := BuildConfig(snaps, "lazy-tcp-proxy")

	// Only the valid entry should produce output
	if len(cfg.HTTP.Routers) != 1 {
		t.Errorf("expected 1 router, got %d", len(cfg.HTTP.Routers))
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
