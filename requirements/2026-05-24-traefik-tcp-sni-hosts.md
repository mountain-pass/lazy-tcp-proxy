# Traefik TCP SNI Routing via `traefik_tcp_hosts`

**Date Added**: 2026-05-24
**Priority**: Medium
**Status**: In Progress

## Problem Statement

REQ-081 added auto-generated `tcp` and `udp` sections to the `/traefik` endpoint, keyed by
listen port. This required users to pre-declare a static Traefik entrypoint for every TCP/UDP
port (`tcp-27015`, `tcp-4444`, etc.), making the Traefik static config grow with every new
service and causing Traefik errors (`EntryPoint doesn't exist`) when entrypoints were missing.

Traefik's existing `websecure` entrypoint (port 443) already supports TCP routing via SNI
inspection — it can read the TLS ClientHello SNI field and route raw TCP connections to
different upstreams by domain name, with no additional static entrypoints required.

This requirement:
1. **Removes** the REQ-081 auto-generated TCP and UDP sections (reverts that approach).
2. **Adds** an explicit `traefik_tcp_hosts` label/config field, parallel to `traefik_hosts`,
   that generates TCP SNI routers on the existing `websecure` entrypoint.

## Functional Requirements

### 1. Remove REQ-081 TCP/UDP auto-generation

The `tcp` and `udp` sections that REQ-081 added to `GET /traefik` are removed. The
`Snapshot` struct in `internal/traefik/config.go` reverts to per-listen-port shape
(`ListenPort int`) and loses `ContainerName`, `TCPPorts`, `UDPPorts`.

The `UDPSnapshot()` method added to `ProxyServer` is removed.

The `/traefik` handler in `main.go` reverts to the simple 1:1 snapshot mapping.

### 2. New `traefik_tcp_hosts` field

Same format as `traefik_hosts`: a list of `"domain:listen_port"` strings.

**Docker label:**
```
lazy-tcp-proxy.traefik-tcp-hosts=mongo.example.com:27015,redis.example.com:6379
```

**YAML config:**
```yaml
services:
  - name: "mongo"
    ports:
      - "27015:27017"
    traefik_tcp_hosts:
      - "mongo.example.com:27015"
```

### 3. Generated Traefik config — TCP section

For each `traefik_tcp_hosts` entry whose port matches the snapshot's `ListenPort`:

- One Traefik TCP **router** with rule `HostSNI(\`<domain>\`)` pointing to a named service.
- One Traefik TCP **service** (load-balancer with a single server `address`:
  `<TRAEFIK_PROXY_HOST>:<listen_port>`).
- `entryPoints`: the same entrypoint as HTTP routers — `TRAEFIK_ENTRYPOINT` (default `websecure`).
- `tls.certResolver`: the same cert resolver as HTTP routers — `TRAEFIK_CERTRESOLVER`
  (default `myresolver`). Traefik terminates TLS and forwards plain TCP to lazy-tcp-proxy.
- Router and service names derived from domain + port, same `sanitiseName` convention as HTTP:
  e.g. `mongo-example-com-27015-router` / `mongo-example-com-27015-service`.

Example generated TCP section for `mongo.example.com:27015`:
```json
"tcp": {
  "routers": {
    "mongo-example-com-27015-router": {
      "entryPoints": ["websecure"],
      "rule": "HostSNI(`mongo.example.com`)",
      "service": "mongo-example-com-27015-service",
      "tls": { "certResolver": "myresolver" }
    }
  },
  "services": {
    "mongo-example-com-27015-service": {
      "loadBalancer": {
        "servers": [{ "address": "lazy-tcp-proxy:27015" }]
      }
    }
  }
}
```

### 4. TCP section absent when empty

If no service has `traefik_tcp_hosts`, the `tcp` key is absent from the response (pointer
field with `omitempty`), same as REQ-081 introduced.

### 5. HTTP section unchanged

`traefik_hosts` and the existing HTTP router generation are completely unaffected. A service
can have both `traefik_hosts` (HTTP) and `traefik_tcp_hosts` (TCP SNI) simultaneously.

### 6. Environment variables

No new env vars. The existing `TRAEFIK_ENTRYPOINT` and `TRAEFIK_CERTRESOLVER` are reused for
TCP routers, keeping the two router types consistent.

## User Experience Requirements

### Docker label

```
lazy-tcp-proxy.traefik-tcp-hosts=mongo.example.com:27015
```

Multiple mappings (comma-separated):
```
lazy-tcp-proxy.traefik-tcp-hosts=mongo.example.com:27015,redis.example.com:6379
```

### YAML config

```yaml
services:
  - name: "mongo"
    ports:
      - "27015:27017"
    traefik_tcp_hosts:
      - "mongo.example.com:27015"
```

### Client usage

Clients connect to the domain on port 443 with TLS. Traefik handles the cert and routes:

```
mongosh "mongodb://mongo.example.com:443/?tls=true"
redis-cli -h redis.example.com -p 443 --tls
```

## Technical Requirements

- `TraefikTCPHosts []string` added to `types.TargetInfo`.
- `traefik_tcp_hosts` label parsed in `backend_docker.go` via `ParseTraefikHosts` (same
  parser already used for `traefik_hosts`).
- `traefik_tcp_hosts` field added to `config.ServiceEntry` (YAML schema).
- `TraefikTCPHosts []string` added to the `traefik.Snapshot` struct.
- `TraefikConfig` gains `TCP *TCPConfig \`json:"tcp,omitempty"\`` (pointer, absent when nil).
- New structs in `internal/traefik/config.go`: `TCPConfig`, `TCPRouter`, `TCPService`,
  `TCPLoadBalancer`, `TCPServer` (same as introduced in REQ-081, minus the UDP ones).
- `BuildConfig` populates the TCP section from `snap.TraefikTCPHosts`, matching by
  `port == snap.ListenPort` (same pattern as HTTP).
- `targetInfoEqual` in `proxy/server.go` updated to include `TraefikTCPHosts`.

## Acceptance Criteria

- [ ] `GET /traefik` no longer contains auto-generated `tcp` or `udp` sections from `ports`/`udp_ports`.
- [ ] A service with `traefik_tcp_hosts: ["mongo.example.com:27015"]` produces a TCP router
      with `rule: HostSNI(\`mongo.example.com\`)`, `entryPoints: ["websecure"]`,
      `tls.certResolver: "myresolver"`, and service address `lazy-tcp-proxy:27015`.
- [ ] Multiple `traefik_tcp_hosts` entries produce one TCP router+service pair each.
- [ ] A service with no `traefik_tcp_hosts` produces no TCP entries.
- [ ] `tcp` key is absent from JSON when no service has `traefik_tcp_hosts`.
- [ ] A service can have both `traefik_hosts` (HTTP) and `traefik_tcp_hosts` (TCP) simultaneously.
- [ ] `TRAEFIK_PROXY_HOST`, `TRAEFIK_ENTRYPOINT`, `TRAEFIK_CERTRESOLVER` apply to TCP routers.
- [ ] Docker label `lazy-tcp-proxy.traefik-tcp-hosts` is parsed identically to `traefik-hosts`.
- [ ] YAML `traefik_tcp_hosts` list behaves identically to the Docker label.
- [ ] Unit tests cover all cases above.

## Out of Scope

- UDP SNI routing (UDP has no SNI concept).
- TLS passthrough mode (Traefik terminates TLS; backend receives plain TCP).
- Per-service override of entrypoint or cert resolver for TCP routers.

## Dependencies

- REQ-069 (Traefik HTTP Provider Endpoint) — TCP section added to same response.
- REQ-081 (Traefik TCP/UDP auto-generation) — **superseded and reverted by this requirement**.
- REQ-001 (Core TCP Proxy) — `ports` already exists on `TargetInfo`.

## Implementation Notes

- REQ-081 reshaped `traefik.Snapshot` to per-container. This requirement reverts it to
  per-listen-port (`ListenPort int`) — simpler and consistent with original HTTP section design.
- `ParseTraefikHosts` in `types/types.go` is reused for `traefik-tcp-hosts` label parsing
  (same `domain:port` format, same validation).
- TCP server uses `address` (`host:port`) not `url` (HTTP uses `url`).
- TCP router rule is `HostSNI(\`domain\`)` not `Host(\`domain\`)`.
