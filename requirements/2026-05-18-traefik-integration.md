# Traefik Integration (HTTP Provider Endpoint)

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Planned

## Problem Statement

lazy-tcp-proxy currently exposes services on fixed TCP ports. Users must know which port maps to
which service and must connect directly to those ports. There is no way to route traffic to a
proxied service by domain name (e.g. `whoami.example.com`) without an additional reverse proxy.

Traefik is a widely used cloud-native reverse proxy. Its free/OSS version (v2/v3) supports an
**HTTP provider** — a pull-based mechanism where Traefik periodically polls an HTTP endpoint to
fetch its dynamic routing configuration. This makes Traefik configurable by a sidecar service
without any writable API or file watching.

By integrating lazy-tcp-proxy with Traefik's HTTP provider, users gain domain-name-based routing
on top of the existing port-based proxying:

```
User → Traefik (80/443) → lazy-tcp-proxy (per-service ports) → proxied Docker service
```

## Functional Requirements

1. **Traefik config endpoint** — lazy-tcp-proxy exposes `GET /traefik` on the existing status
   HTTP server (port `STATUS_PORT`, default 8080). The response is a Traefik v3-compatible HTTP
   provider JSON payload.

2. **Per-service domain configuration** — each proxied service can declare one or more Traefik
   host rules via:
   - Docker label: `lazy-tcp-proxy.traefik-host=myapp.example.com` (applies to all ports of that
     service; if the service has multiple ports, the first listen port is used).
   - Docker label (per-port): `lazy-tcp-proxy.traefik-host.9001=myapp.example.com` where `9001`
     is the listen port.
   - YAML config field: `traefik_host: "myapp.example.com"` (single-port shorthand) or
     `traefik_hosts: { 9001: "myapp.example.com", 9002: "admin.example.com" }` (per-port map).

3. **Backend URL construction** — when generating the Traefik config, lazy-tcp-proxy constructs
   backend service URLs as `http://<TRAEFIK_PROXY_HOST>:<listen_port>`. The host is configurable
   via the `TRAEFIK_PROXY_HOST` environment variable (default: the value of `hostname()`).

4. **Generated Traefik config shape** — for each registered service port with a `traefik_host`:
   - One Traefik HTTP **router** with rule `Host(\`<traefik_host>\`)`, pointing to a named service,
     and attached to all configured entry points (default: `web`; configurable via
     `TRAEFIK_ENTRYPOINTS` env var, comma-separated list, e.g. `web,websecure`).
   - One Traefik HTTP **service** (load-balancer with a single server URL:
     `http://<TRAEFIK_PROXY_HOST>:<listen_port>`).
   - Router and service names are derived from the container name + listen port, e.g.
     `whoami-9001-router` / `whoami-9001-service`.

5. **Empty config** — if no registered services have `traefik_host` set, `GET /traefik` returns
   a valid empty Traefik HTTP provider JSON (`{"http":{"routers":{},"services":{}}}`).

6. **Docker Compose example** — a new `example/traefik/` directory containing:
   - `docker-compose.yml` — starts Traefik + lazy-tcp-proxy + an example `whoami` service.
   - Traefik is configured to poll `http://lazy-tcp-proxy:8080/traefik` every 5 seconds.
   - The whoami service has `lazy-tcp-proxy.traefik-host=whoami.localhost` set.
   - `TRAEFIK_PROXY_HOST=lazy-tcp-proxy` is set on lazy-tcp-proxy.
   - A README explaining how to test the setup.

## User Experience Requirements

### Docker label configuration

Single-port service (all ports inherit the same host — uses first listen port):
```
lazy-tcp-proxy.traefik-host=myapp.localhost
```

Per-port label (when a service has multiple listen ports):
```
lazy-tcp-proxy.traefik-host.9001=myapp.localhost
lazy-tcp-proxy.traefik-host.9002=admin.localhost
```

### YAML config file

```yaml
services:
  - name: "whoami"
    ports:
      - "9001:80"
    traefik_host: "whoami.localhost"

  - name: "multi-port-app"
    ports:
      - "9001:80"
      - "9002:8080"
    traefik_hosts:
      9001: "myapp.localhost"
      9002: "admin.localhost"
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `TRAEFIK_PROXY_HOST` | system hostname | Hostname/IP Traefik uses to reach lazy-tcp-proxy |
| `TRAEFIK_ENTRYPOINTS` | `web` | Comma-separated list of Traefik entry point names |

### Example Traefik static config (traefik.yml)

```yaml
providers:
  http:
    endpoint: "http://lazy-tcp-proxy:8080/traefik"
    pollInterval: "5s"
```

## Technical Requirements

- Response `Content-Type: application/json`.
- The endpoint is unauthenticated (same as `/status` — network-level access control is expected).
- Router/service names must be valid Traefik identifiers: lowercase alphanumeric + hyphens.
  Container names that contain underscores or other characters are sanitised.
- The endpoint reflects the live in-memory state of the proxy server (same source as `/status`).
- `traefik_host` and `traefik_hosts` are optional YAML fields; existing configs without them are
  completely unaffected.
- Traefik v3 JSON shape is used (HTTP provider format is identical between v2 and v3).

## Acceptance Criteria

- [ ] `GET /traefik` returns `200 OK` with `Content-Type: application/json`.
- [ ] Response is valid Traefik HTTP provider JSON (routers + services objects).
- [ ] A service with `lazy-tcp-proxy.traefik-host=whoami.localhost` and listen port 9001 produces
      a router with rule `Host(\`whoami.localhost\`)` and a service URL `http://<host>:9001`.
- [ ] A service with per-port labels (`lazy-tcp-proxy.traefik-host.9001=a.localhost`) produces
      one router/service per labelled port.
- [ ] A service with no `traefik_host` label/field does not appear in the Traefik config.
- [ ] `TRAEFIK_PROXY_HOST=my-host` causes service URLs to use `http://my-host:<port>`.
- [ ] `TRAEFIK_ENTRYPOINTS=web,websecure` causes routers to include both entry points.
- [ ] YAML fields `traefik_host` and `traefik_hosts` behave identically to their label equivalents.
- [ ] `GET /traefik` returns valid empty JSON when no service has a `traefik_host`.
- [ ] The `example/traefik/docker-compose.yml` starts successfully with `docker compose up`.
- [ ] `curl -H "Host: whoami.localhost" http://localhost` returns a response from the whoami
      container (routed via Traefik → lazy-tcp-proxy → whoami).
- [ ] Existing behaviour of all other endpoints and services is unaffected.

## Dependencies

- REQ-025 (HTTP Status Endpoint) — the `/traefik` endpoint is added to the same HTTP mux.
- REQ-065 (Dynamic Configuration File) — `traefik_host` / `traefik_hosts` are added as fields to
  the existing YAML service schema (`ServiceEntry` in `internal/config/store.go`).
- REQ-001 (Core TCP Proxy) — the Traefik config reflects the live proxy state
  (`ProxyServer.Snapshot()`).

## Implementation Notes

- New package: `internal/traefik/` — `config.go` with `BuildConfig(snapshots []types.Snapshot,
  proxyHost string, entryPoints []string) TraefikConfig` function.
- `TraefikConfig` is a Go struct that marshals cleanly to the Traefik HTTP provider JSON shape.
- `traefik_host` / `traefik_hosts` fields are added to:
  - `types.TargetInfo` (runtime state)
  - `config.ServiceEntry` (YAML schema)
  - Docker label parser in `backend_docker.go`
- The `/traefik` HTTP handler calls `srv.Snapshot()` then `traefik.BuildConfig(...)`.
- Name sanitisation: replace `_` with `-`, strip non-alphanumeric/hyphen chars, lowercase.
- The Docker Compose example uses `traefik:v3` image with a minimal `traefik.yml` static config
  mounted as a volume, and a `whoami` service labelled with `lazy-tcp-proxy.traefik-host`.
