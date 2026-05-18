# Traefik Integration — Implementation Plan

**Requirement**: [2026-05-18-traefik-integration.md](2026-05-18-traefik-integration.md)
**Date**: 2026-05-18
**Status**: Draft

## Implementation Steps

1. Add `TraefikHosts []string` to `types.TargetInfo` and a `ParseTraefikHosts` helper in
   `internal/types/types.go`.

2. Add `TraefikHosts []string` to `config.ServiceEntry` in `internal/config/store.go` and
   copy it through in `entryToTargetInfo`.

3. Parse the `lazy-tcp-proxy.traefik-hosts` Docker label in **both** the container path and the
   Swarm service path in `internal/docker/manager.go`.

4. Add `TraefikHosts []string` to `proxy.TargetSnapshot`, populate it in `Snapshot()`, and
   add it to `targetInfoEqual` in `internal/proxy/server.go`.

5. Create `internal/traefik/config.go` — pure config-building package with its own `Snapshot`
   input type and `BuildConfig` function.

6. Wire the `/traefik` HTTP handler and `TRAEFIK_PROXY_HOST` env var into `main.go`.

7. Create `example/traefik/` with `docker-compose.yml`, `traefik.yml`, and `README.md`.

8. Add unit tests in `internal/traefik/config_test.go`.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | Add `TraefikHosts []string` to `TargetInfo`; add `ParseTraefikHosts` |
| `internal/config/store.go` | Modify | Add `TraefikHosts` to `ServiceEntry`; copy in `entryToTargetInfo` |
| `internal/docker/manager.go` | Modify | Parse `lazy-tcp-proxy.traefik-hosts` label in container + swarm paths |
| `internal/proxy/server.go` | Modify | Add `TraefikHosts` to `TargetSnapshot`; update `Snapshot()` and `targetInfoEqual` |
| `internal/traefik/config.go` | Create | `Snapshot` input type, Go structs for Traefik JSON, `BuildConfig()` |
| `internal/traefik/config_test.go` | Create | Unit tests for `BuildConfig` and `sanitiseName` |
| `main.go` | Modify | Add `/traefik` handler to `runStatusServer`; add `resolveTraefikProxyHost` |
| `example/traefik/docker-compose.yml` | Create | Traefik + lazy-tcp-proxy + whoami example |
| `example/traefik/traefik.yml` | Create | Minimal Traefik static config |
| `example/traefik/README.md` | Create | Setup and test instructions |

---

## API Contracts

### `GET /traefik`

**Response** `200 OK`, `Content-Type: application/json`

```json
{
  "http": {
    "routers": {
      "whoami-localhost-9001-router": {
        "rule": "Host(`whoami.localhost`)",
        "service": "whoami-localhost-9001-service"
      }
    },
    "services": {
      "whoami-localhost-9001-service": {
        "loadBalancer": {
          "servers": [
            { "url": "http://lazy-tcp-proxy:9001" }
          ]
        }
      }
    }
  }
}
```

Empty (no `traefik_hosts` configured on any service):
```json
{"http":{"routers":{},"services":{}}}
```

---

## Data Models

### `internal/traefik/config.go` types

```go
// Snapshot is the minimal per-listen-port info BuildConfig needs.
// Defined here so the traefik package does not import proxy.
type Snapshot struct {
    ListenPort   int
    TraefikHosts []string // e.g. ["whoami.localhost:9001", "app.localhost:9002"]
}

type TraefikConfig struct {
    HTTP HTTPConfig `json:"http"`
}

type HTTPConfig struct {
    Routers  map[string]HTTPRouter  `json:"routers"`
    Services map[string]HTTPService `json:"services"`
}

type HTTPRouter struct {
    Rule    string `json:"rule"`
    Service string `json:"service"`
}

type HTTPService struct {
    LoadBalancer LoadBalancer `json:"loadBalancer"`
}

type LoadBalancer struct {
    Servers []Server `json:"servers"`
}

type Server struct {
    URL string `json:"url"`
}
```

### `traefik_hosts` entry format

Each entry is a string `"<domain>:<listen_port>"`.  
Split strategy: `strings.LastIndex(entry, ":")` — handles domains that might theoretically
contain dots but no colons; the port is always the last colon-separated token.

---

## Key Code Snippets

### `ParseTraefikHosts` (types package)

```go
func ParseTraefikHosts(label, s string) []string {
    var out []string
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
        if _, err := strconv.Atoi(entry[idx+1:]); err != nil {
            log.Printf("label %s: ignoring invalid traefik-hosts entry %q: port must be integer", label, entry)
            continue
        }
        out = append(out, entry)
    }
    return out
}
```

### `sanitiseName` (traefik package)

```go
func sanitiseName(s string) string {
    s = strings.ToLower(s)
    var b strings.Builder
    for _, r := range s {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
            b.WriteRune(r)
        } else {
            b.WriteRune('-')
        }
    }
    // Collapse consecutive hyphens; trim leading/trailing
    result := strings.TrimFunc(
        regexp.MustCompile(`-{2,}`).ReplaceAllString(b.String(), "-"),
        func(r rune) bool { return r == '-' },
    )
    return result
}
```

### `BuildConfig` (traefik package)

```go
func BuildConfig(snapshots []Snapshot, proxyHost string) TraefikConfig {
    routers := map[string]HTTPRouter{}
    services := map[string]HTTPService{}

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
            routers[routerName] = HTTPRouter{
                Rule:    fmt.Sprintf("Host(`%s`)", domain),
                Service: serviceName,
            }
            services[serviceName] = HTTPService{
                LoadBalancer: LoadBalancer{
                    Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, port)}},
                },
            }
        }
    }

    return TraefikConfig{HTTP: HTTPConfig{Routers: routers, Services: services}}
}
```

### `resolveTraefikProxyHost` (main.go)

```go
func resolveTraefikProxyHost() string {
    if v := os.Getenv("TRAEFIK_PROXY_HOST"); v != "" {
        return v
    }
    return "lazy-tcp-proxy"
}
```

### `/traefik` handler wiring (main.go — inside `runStatusServer`)

```go
traefikProxyHost := resolveTraefikProxyHost()
mux.HandleFunc("/traefik", func(w http.ResponseWriter, r *http.Request) {
    raw := srv.Snapshot()
    snaps := make([]traefikpkg.Snapshot, len(raw))
    for i, s := range raw {
        snaps[i] = traefikpkg.Snapshot{ListenPort: s.ListenPort, TraefikHosts: s.TraefikHosts}
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(traefikpkg.BuildConfig(snaps, traefikProxyHost)) //nolint:errcheck
})
```

`runStatusServer` gains a `traefikProxyHost string` parameter.

---

## Unit Tests

### `internal/traefik/config_test.go`

| Test | Input | Expected Output |
|------|-------|-----------------|
| Single host, matching port | `Snapshot{9001, ["whoami.localhost:9001"]}`, host=`lazy-tcp-proxy` | Router `Host(\`whoami.localhost\`)`, service URL `http://lazy-tcp-proxy:9001` |
| Entry port mismatch | `Snapshot{9001, ["whoami.localhost:9002"]}` | Empty routers/services |
| Multiple hosts on same snapshot | `Snapshot{9001, ["a.localhost:9001","b.localhost:9001"]}` | Two routers, two services |
| Multiple snapshots | Two snapshots with different ports | Two router/service pairs |
| No hosts | `Snapshot{9001, nil}` | `{"http":{"routers":{},"services":{}}}` |
| Name sanitisation | domain `my_app.localhost`, port `9001` | Name `my-app-localhost-9001-router` |
| TRAEFIK_PROXY_HOST override | host=`10.0.0.5` | Service URL `http://10.0.0.5:9001` |
| Malformed entry skipped | `"noport"`, `"bad:xyz"` | Entry skipped, no panic |

---

## `example/traefik/docker-compose.yml` sketch

```yaml
name: example-traefik

services:

  traefik:
    image: traefik:v3
    ports:
      - "80:80"
    volumes:
      - ./traefik.yml:/etc/traefik/traefik.yml:ro
    networks:
      - traefik-net

  lazy-tcp-proxy:
    image: mountainpass/lazy-tcp-proxy
    environment:
      - DOCKER_SOCK=/var/run/docker.sock
      - IDLE_TIMEOUT_SECS=30
      - TRAEFIK_PROXY_HOST=lazy-tcp-proxy
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    ports:
      - "8080:8080"   # status + /traefik endpoint
      - "9001:9001"   # whoami listen port
    networks:
      - traefik-net
    restart: always

  whoami:
    image: traefik/whoami
    container_name: example-traefik-whoami
    networks:
      - traefik-private
    labels:
      - "lazy-tcp-proxy.enabled=true"
      - "lazy-tcp-proxy.ports=9001:80"
      - "lazy-tcp-proxy.traefik-hosts=whoami.localhost:9001"

networks:
  traefik-net:
    driver: bridge
  traefik-private:
    driver: bridge
    internal: true
```

`traefik.yml`:
```yaml
entryPoints:
  web:
    address: ":80"

providers:
  http:
    endpoint: "http://lazy-tcp-proxy:8080/traefik"
    pollInterval: "5s"
```

> **Note on networks**: Traefik and lazy-tcp-proxy share `traefik-net` so Traefik can reach
> lazy-tcp-proxy on port 9001. The whoami container is on `traefik-private` (internal) — only
> lazy-tcp-proxy (which Docker-joins that network at runtime) can reach it.

---

## Risks & Open Questions

- **`sanitiseName` collisions**: two different domains could produce the same sanitised name
  (e.g. `my-app.localhost` and `my_app.localhost`). Low probability; last-write-wins in the map.
  A future enhancement could append a short hash suffix on collision detection.
- **`runStatusServer` signature change**: adding `traefikProxyHost` changes the function
  signature. The function is internal (lowercase caller in `main.go` only), so no external impact.
- **Port range services**: `traefik_hosts` entries for ports within a range work correctly because
  each port in the range gets its own `TargetSnapshot` with the correct `ListenPort`; the entry is
  matched by the `port == snap.ListenPort` guard in `BuildConfig`.
