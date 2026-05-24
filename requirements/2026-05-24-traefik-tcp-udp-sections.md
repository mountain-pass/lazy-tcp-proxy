# Traefik TCP and UDP Provider Sections

**Date Added**: 2026-05-24
**Priority**: Medium
**Status**: Planned

## Problem Statement

The `/traefik` HTTP provider endpoint (REQ-069) currently only emits an `http` section.
Traefik's dynamic config format also supports `tcp` and `udp` sections, enabling Traefik to
forward raw TCP and UDP traffic to lazy-tcp-proxy's listen ports.

Users who run services on TCP or UDP ports (e.g. DNS on UDP/53, databases on TCP/5432) have no
way to route those ports through Traefik today. The listen-port assignments already exist in
`ports` (TCP) and `udp_ports` (UDP) — this requirement auto-generates the Traefik TCP and UDP
sections from that existing data, requiring no new labels or config fields.

## Functional Requirements

### 1. Auto-generate TCP section from `ports`

For every service that has at least one `ports` (TCP) mapping, emit one Traefik TCP router +
service pair per listen port into the `tcp` section of the `/traefik` response.

- **Router rule**: `HostSNI(\`*\`)` (catch-all; no TLS/SNI domain discrimination needed)
- **EntryPoint**: `tcp-<listen_port>` (convention; user defines this in Traefik static config)
- **Service address**: `<TRAEFIK_PROXY_HOST>:<listen_port>`
- **Name scheme**: `<sanitised-service-name>-tcp-<listen_port>-router` / `-service`

### 2. Auto-generate UDP section from `udp_ports`

For every service that has at least one `udp_ports` mapping, emit one Traefik UDP router +
service pair per listen port into the `udp` section of the `/traefik` response.

- **No rule** (Traefik UDP routers have no rule field)
- **EntryPoint**: `udp-<listen_port>` (convention; user defines this in Traefik static config)
- **Service address**: `<TRAEFIK_PROXY_HOST>:<listen_port>`
- **Name scheme**: `<sanitised-service-name>-udp-<listen_port>-router` / `-service`

### 3. Entrypoint naming convention

The generated config uses `tcp-<port>` and `udp-<port>` as entrypoint names. Users must
declare matching entrypoints in Traefik's static config, e.g.:

```yaml
entryPoints:
  tcp-5432:
    address: ":5432/tcp"
  udp-53:
    address: ":53/udp"
```

TCP and UDP can share the same port number (e.g. `tcp-53` and `udp-53` for DNS) — they are
independent at the network and Traefik entrypoint level.

### 4. Response shape

`GET /traefik` response extended to include `tcp` and `udp` top-level keys when at least one
entry exists for that protocol. Empty sections are omitted from the response (not emitted as
`"tcp":{}` etc.).

Example with a DNS service (`udp_ports: ["53:53"]`) and a Postgres service (`ports: ["5432:5432"]`):

```json
{
  "http": { "routers": {}, "services": {} },
  "tcp": {
    "routers": {
      "postgres-tcp-5432-router": {
        "entryPoints": ["tcp-5432"],
        "rule": "HostSNI(`*`)",
        "service": "postgres-tcp-5432-service"
      }
    },
    "services": {
      "postgres-tcp-5432-service": {
        "loadBalancer": { "servers": [{ "address": "lazy-tcp-proxy:5432" }] }
      }
    }
  },
  "udp": {
    "routers": {
      "dns-udp-53-router": {
        "entryPoints": ["udp-53"],
        "service": "dns-udp-53-service"
      }
    },
    "services": {
      "dns-udp-53-service": {
        "loadBalancer": { "servers": [{ "address": "lazy-tcp-proxy:53" }] }
      }
    }
  }
}
```

### 5. Service name for router/service naming

The sanitised service/container name is used as the prefix in the router and service names.
The same `sanitiseName` function used for HTTP entries applies.

### 6. Existing `traefik_hosts` (HTTP section) is unaffected

HTTP routers generated from `traefik_hosts` continue to work exactly as before. TCP/UDP
generation is additive and independent.

### 7. Empty response unchanged

If no services have `ports` or `udp_ports`, the `tcp` and `udp` keys are absent from the
response (same behaviour as today for `http` — though `http` keeps its empty `routers`/`services`
maps for backwards compatibility).

## User Experience Requirements

No new labels or config fields are required. As soon as a service has `ports` or `udp_ports`
configured, its listen ports appear in the Traefik response automatically.

Users must add matching entrypoints to `traefik.yml` once per port — the convention is predictable
enough that this is straightforward.

## Technical Requirements

- Extend the `traefik.Snapshot` struct with `ContainerName string`, `TCPPorts []int`, `UDPPorts []int`.
- Extend `BuildConfig` to populate `TCPConfig` and `UDPConfig` sections in `TraefikConfig`.
- Add `TCPConfig`, `UDPConfig`, `TCPRouter`, `TCPService`, `UDPRouter`, `UDPService`,
  `TCPLoadBalancer`, `UDPLoadBalancer`, `TCPServer`, `UDPServer` structs to `internal/traefik/config.go`.
- Update the snapshot builder in `internal/proxy/server.go` (or wherever snapshots are assembled
  for `/traefik`) to populate `ContainerName`, `TCPPorts`, and `UDPPorts`.
- `TCPConfig` and `UDPConfig` fields on `TraefikConfig` use `omitempty` so absent sections are not
  serialised.
- The `TRAEFIK_PROXY_HOST` env var (default `lazy-tcp-proxy`) is reused for TCP and UDP server addresses.

## Acceptance Criteria

- [ ] A service with `ports: ["5432:5432"]` produces a TCP router `HostSNI(\`*\`)` on entrypoint
      `tcp-5432` pointing to `lazy-tcp-proxy:5432` in `GET /traefik`.
- [ ] A service with `udp_ports: ["53:53"]` produces a UDP router on entrypoint `udp-53`
      pointing to `lazy-tcp-proxy:53` in `GET /traefik`.
- [ ] A service with both `ports` and `udp_ports` on the same port number (e.g. `53`) produces
      both a TCP and a UDP entry.
- [ ] Multiple TCP ports on one service produce one router+service pair each.
- [ ] Multiple UDP ports on one service produce one router+service pair each.
- [ ] A service with no `ports` and no `udp_ports` produces no TCP or UDP entries.
- [ ] `tcp` and `udp` keys are absent from the JSON when there are no entries for that protocol.
- [ ] Existing `traefik_hosts` HTTP generation is unaffected.
- [ ] `TRAEFIK_PROXY_HOST=my-host` causes TCP and UDP service addresses to use `my-host:<port>`.
- [ ] Unit tests cover all cases above.

## Dependencies

- REQ-069 (Traefik HTTP Provider Endpoint) — this requirement extends it.
- REQ-027 (UDP Traffic Support) — `udp_ports` already exists on `TargetInfo`.
- REQ-001 (Core TCP Proxy) — `ports` already exists on `TargetInfo`.

## Implementation Notes

- `TCPConfig` and `UDPConfig` must be pointer fields (or use `omitempty` on map) on `TraefikConfig`
  so they serialise to absent rather than `null`/`{}` when empty.
- Traefik TCP `HostSNI('*')` requires the entrypoint to **not** have TLS configured in static config
  (or to use `tls: {}` passthrough mode). Document this in the example.
- UDP routers have no `rule` field at all in Traefik's schema — omit it entirely.
