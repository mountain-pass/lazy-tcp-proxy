# Traefik Integration (HTTP Provider Endpoint)

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Planned

## Problem Statement

lazy-tcp-proxy currently exposes services on fixed TCP ports. Users must know which port maps to
which service and connect directly to those ports. There is no way to route traffic to a proxied
service by domain name (e.g. `whoami.example.com`) without additional reverse-proxy configuration.

Traefik is a widely used cloud-native reverse proxy. Its free/OSS version (v2/v3) supports an
**HTTP provider** — a pull-based mechanism where Traefik periodically polls an HTTP endpoint to
fetch its dynamic routing configuration. This makes Traefik configurable by a sidecar service
without any writable API or file watching.

By integrating lazy-tcp-proxy with Traefik's HTTP provider, users gain domain-name-based HTTP
routing on top of the existing port-based proxying:

```
User → Traefik (80/443) → lazy-tcp-proxy (per-service ports) → proxied Docker service
```

## Functional Requirements

### 1. Traefik config endpoint

`GET /traefik` on the existing status HTTP server (port `STATUS_PORT`, default 8080).

The response is a Traefik v3-compatible HTTP provider JSON payload containing `http`, `tcp`, and
`udp` sections as appropriate (see below). `Content-Type: application/json`.

### 2. `traefik_hosts` — per-service domain-to-port mapping

Each proxied service can declare Traefik host mappings as a list of `"domain:listen_port"` strings.
`listen_port` is the port lazy-tcp-proxy listens on (the left side of a port mapping).

**Docker label:**
```
lazy-tcp-proxy.traefik-hosts=myapp.localhost:9000,myapp2.localhost:9001
```

**YAML config:**
```yaml
services:
  - name: "my-container"
    ports:
      - "9000-9099:9000-9099"
    traefik_hosts:
      - "myapp.localhost:9000"
      - "myapp2.localhost:9001"
```

This format reads left-to-right ("this domain routes to this listen port") and naturally handles
port ranges by letting users list only the specific mappings they need.

### 3. Generated Traefik config — HTTP section

For each `traefik_hosts` entry on a TCP port:
- One Traefik HTTP **router** with rule `Host(\`<domain>\`)` and `service` pointing to a named
  load-balancer.
- One Traefik HTTP **service** (load-balancer with a single server URL:
  `http://<TRAEFIK_PROXY_HOST>:<listen_port>`).
- Router and service names derived from the domain + listen port, sanitised to lowercase
  alphanumeric + hyphens, e.g. `myapp-localhost-9000-router` / `myapp-localhost-9000-service`.
- `entryPoints` is **omitted** from the router — Traefik applies the router to all defined entry
  points by default, keeping lazy-tcp-proxy decoupled from Traefik's static configuration.

### 4. Generated Traefik config — TCP section

Traefik TCP routers use `HostSNI` rules. Domain-based TCP routing requires TLS (SNI inspection).
For non-TLS TCP, only `HostSNI('*')` (catch-all) is possible, providing no domain routing benefit.

**Scope for this feature**: TCP entries in `traefik_hosts` that correspond to a port also
configured as `https: true` (TLS-terminated by lazy-tcp-proxy) are treated as HTTP mappings
(Traefik sees plain HTTP). Raw TCP ports without TLS are **not** represented in the Traefik TCP
section — there is no useful domain routing to offer. This is noted as a future extension.

### 5. Generated Traefik config — UDP section

Traefik UDP routers have no concept of rules or domain matching — they are purely entry-point-based
(port-based). The architecture `User → Traefik → lazy-tcp-proxy → container` for UDP would only
shift port management to Traefik's static config without enabling domain routing.

**Scope for this feature**: UDP ports are **not** represented in the generated Traefik config.
This is noted as a future extension if there is demand for unified port management.

### 6. Middleware overlap — out of scope

lazy-tcp-proxy implements its own per-service `allow_list` and `block_list` (IP-based). Traefik
has an equivalent IP allowlist middleware. Generating Traefik middleware from lazy-tcp-proxy's
allow/block lists would be redundant (lazy-tcp-proxy enforces them regardless) and would couple
the two systems' configurations.

**Decision**: lazy-tcp-proxy's allow/block lists remain the source of truth and are enforced at
the proxy layer. Generating Traefik middleware from them is left as a future enhancement.

### 7. Empty config

If no registered services have `traefik_hosts` set, `GET /traefik` returns a valid empty payload:
```json
{"http":{"routers":{},"services":{}}}
```

### 8. Environment variables

| Variable | Default | Description |
|---|---|---|
| `TRAEFIK_PROXY_HOST` | `lazy-tcp-proxy` | Hostname/IP Traefik uses to reach lazy-tcp-proxy |

`TRAEFIK_PROXY_HOST` must be set to the DNS name or IP address that Traefik can use to reach
lazy-tcp-proxy's listen ports. It defaults to `lazy-tcp-proxy`, matching the conventional Docker
Compose service name. Users on non-Docker setups (bare metal, custom names) must override it.

### 9. Docker Compose example

A new `example/traefik/` directory containing:
- `docker-compose.yml` — Traefik + lazy-tcp-proxy + a `whoami` example service.
  - Traefik polls `http://lazy-tcp-proxy:8080/traefik` every 5 seconds.
  - `TRAEFIK_PROXY_HOST=lazy-tcp-proxy` set on lazy-tcp-proxy.
  - The whoami service labelled with `lazy-tcp-proxy.traefik-hosts=whoami.localhost:9001`.
  - Traefik listens on port 80 (`web` entry point).
- `traefik.yml` — minimal Traefik static config (HTTP provider + entry point declaration).
- `README.md` — how to start and test (`curl -H "Host: whoami.localhost" http://localhost`).

## User Experience Requirements

### YAML config

```yaml
services:
  - name: "whoami"
    ports:
      - "9001:80"
    traefik_hosts:
      - "whoami.localhost:9001"

  - name: "range-app"
    ports:
      - "9000-9099:9000-9099"
    traefik_hosts:
      - "app1.localhost:9000"
      - "app2.localhost:9005"
```

### Docker label

```
lazy-tcp-proxy.traefik-hosts=whoami.localhost:9001
```

Multiple mappings (comma-separated, consistent with `lazy-tcp-proxy.ports`):
```
lazy-tcp-proxy.traefik-hosts=app1.localhost:9000,app2.localhost:9005
```

### Traefik static config (`traefik.yml`)

```yaml
entryPoints:
  web:
    address: ":80"

providers:
  http:
    endpoint: "http://lazy-tcp-proxy:8080/traefik"
    pollInterval: "5s"
```

## Technical Requirements

- Response `Content-Type: application/json`.
- The endpoint is unauthenticated (same as `/status`; network-level access control expected).
- Router/service names are sanitised: lowercase, replace non-alphanumeric chars with `-`, collapse
  consecutive hyphens, trim leading/trailing hyphens.
- The endpoint reflects live in-memory proxy state (same source as `/status`).
- `traefik_hosts` is an optional field; existing configs without it are completely unaffected.
- Traefik v3 JSON shape is used (identical between v2 and v3 for the HTTP provider).

## Acceptance Criteria

- [ ] `GET /traefik` returns `200 OK` with `Content-Type: application/json`.
- [ ] Response is valid Traefik HTTP provider JSON.
- [ ] A service with `lazy-tcp-proxy.traefik-hosts=whoami.localhost:9001` produces a router with
      rule `Host(\`whoami.localhost\`)` and service URL `http://lazy-tcp-proxy:9001`.
- [ ] Multiple `traefik_hosts` entries produce one router+service pair each.
- [ ] A service with no `traefik_hosts` does not appear in `GET /traefik` output.
- [ ] `TRAEFIK_PROXY_HOST=my-host` causes service URLs to use `http://my-host:<port>`.
- [ ] `entryPoints` is absent from generated routers (Traefik defaults to all entry points).
- [ ] YAML `traefik_hosts` list behaves identically to the Docker label.
- [ ] `GET /traefik` returns valid empty JSON when no service has `traefik_hosts`.
- [ ] `example/traefik/docker-compose.yml` starts successfully with `docker compose up`.
- [ ] `curl -H "Host: whoami.localhost" http://localhost` returns a response from the whoami
      container (routed via Traefik → lazy-tcp-proxy → whoami).
- [ ] Existing behaviour of all other endpoints and services is unaffected.

## Out of Scope / Future Extensions

- **Traefik TCP section**: SNI-based domain routing for raw TCP services (requires TLS on the
  connection end-to-end, with Traefik in passthrough mode).
- **Traefik UDP section**: UDP has no domain routing in Traefik; entry-point-based generation
  could be added if users want unified port management via Traefik.
- **Middleware generation**: Generating Traefik IP allowlist/blocklist middleware from
  lazy-tcp-proxy's `allow_list`/`block_list` per service.

## Dependencies

- REQ-025 (HTTP Status Endpoint) — `/traefik` is added to the same HTTP mux.
- REQ-065 (Dynamic Configuration File) — `traefik_hosts` added as a field to `ServiceEntry`.
- REQ-001 (Core TCP Proxy) — the Traefik config reflects live proxy state.

## Implementation Notes

- New package: `internal/traefik/` — `config.go` with
  `BuildConfig(snapshots []proxy.Snapshot, proxyHost string) TraefikConfig`.
- `TraefikConfig` is a Go struct that marshals to the Traefik HTTP provider JSON shape.
- `traefik_hosts` field added to:
  - `types.TargetInfo` (runtime state) as `[]string`
  - `config.ServiceEntry` (YAML schema) as `[]string`
  - Docker label parser in `backend_docker.go`
- Parsing `traefik_hosts` entries: split on `,` for labels; each entry is `domain:port` —
  split on last `:` to extract port (accommodating IPv6 or dotted domains).
- The `/traefik` HTTP handler calls `srv.Snapshot()` then `traefik.BuildConfig(...)`.
- Name sanitisation function shared between router and service name generation.
