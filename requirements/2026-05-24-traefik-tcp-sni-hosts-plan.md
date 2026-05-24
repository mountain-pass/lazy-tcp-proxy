# Traefik TCP SNI Routing via `traefik_tcp_hosts` — Implementation Plan

**Requirement**: [2026-05-24-traefik-tcp-sni-hosts.md](2026-05-24-traefik-tcp-sni-hosts.md)
**Date**: 2026-05-24
**Status**: Draft

## Implementation Steps

1. **Revert REQ-081 from `internal/traefik/config.go`**
   - Remove `ContainerName`, `TCPPorts`, `UDPPorts` from `Snapshot`; restore `ListenPort int`.
   - Remove `TCPConfig`, `TCPRouter`, `TCPService`, `TCPLoadBalancer`, `TCPServer` structs
     (will be re-added in step 4 with different wiring).
   - Remove `UDPConfig`, `UDPRouter`, `UDPService`, `UDPLoadBalancer`, `UDPServer` structs
     (not needed — UDP SNI is out of scope).
   - Remove `TCP` and `UDP` fields from `TraefikConfig`.
   - Remove `containsInt` helper.
   - Restore `BuildConfig` HTTP section to use `port != snap.ListenPort` guard.

2. **Revert REQ-081 from `internal/proxy/server.go`**
   - Remove `UDPSnapshot()` method.

3. **Revert REQ-081 from `main.go`**
   - Restore `/traefik` handler to simple 1:1 mapping:
     `snaps[i] = traefikpkg.Snapshot{ListenPort: s.ListenPort, TraefikHosts: s.TraefikHosts}`

4. **`internal/traefik/config.go` — add TCP SNI structs**
   - Add `TCP *TCPConfig \`json:"tcp,omitempty"\`` to `TraefikConfig`.
   - Add structs: `TCPConfig`, `TCPRouter`, `TCPService`, `TCPLoadBalancer`, `TCPServer`.
     ```go
     type TCPConfig struct {
         Routers  map[string]TCPRouter  `json:"routers"`
         Services map[string]TCPService `json:"services"`
     }
     type TCPRouter struct {
         EntryPoints []string   `json:"entryPoints,omitempty"`
         Rule        string     `json:"rule"`
         Service     string     `json:"service"`
         TLS         *RouterTLS `json:"tls,omitempty"`
     }
     type TCPService struct {
         LoadBalancer TCPLoadBalancer `json:"loadBalancer"`
     }
     type TCPLoadBalancer struct {
         Servers []TCPServer `json:"servers"`
     }
     type TCPServer struct {
         Address string `json:"address"`
     }
     ```
   - Note: `TCPRouter` reuses the existing `RouterTLS` struct (same shape).
   - Add `TraefikTCPHosts []string` to `Snapshot`.

5. **`internal/traefik/config.go` — extend `BuildConfig` with TCP section**
   - After the HTTP loop, add a TCP loop: for each `snap.TraefikTCPHosts` entry, extract
     `domain:port`, match `port == snap.ListenPort`, emit one `TCPRouter` + `TCPService`.
   - Router: `Rule: "HostSNI(\`<domain>\`)"`, `Service: <name>-service`.
   - EntryPoints and TLS follow same logic as HTTP: apply when `entryPoint`/`certResolver`
     non-empty. For TCP SNI routing, `certResolver` is almost always set (TLS required for SNI).
   - Service: `address: "<proxyHost>:<port>"` (not `url`).
   - Name scheme: `sanitiseName("<domain>-<port>")` + `-router`/`-service` — identical
     to HTTP name scheme; no collision because they live in separate `http`/`tcp` sections.
   - If no TCP entries generated, leave `cfg.TCP` nil (absent from JSON).

6. **`internal/types/types.go` — add `TraefikTCPHosts` to `TargetInfo`**
   - Add `TraefikTCPHosts []string` field after `TraefikHosts`.

7. **`internal/docker/manager.go` — parse `traefik-tcp-hosts` label**
   - Two locations (container inspect and event-based label scan), both follow the same
     pattern as `traefik-hosts`:
     ```go
     var traefikTCPHosts []string
     if v := strings.TrimSpace(labels["lazy-tcp-proxy.traefik-tcp-hosts"]); v != "" {
         traefikTCPHosts = types.ParseTraefikHosts("lazy-tcp-proxy.traefik-tcp-hosts", v)
     }
     ```
   - Add `TraefikTCPHosts: traefikTCPHosts` to the returned `TargetInfo`.

8. **`internal/config/store.go` — add `TraefikTCPHosts` to `ServiceEntry`**
   - Add `TraefikTCPHosts []string \`yaml:"traefik_tcp_hosts,omitempty" json:"traefik_tcp_hosts,omitempty"\`` after `TraefikHosts`.
   - Add `info.TraefikTCPHosts = entry.TraefikTCPHosts` where config entries are mapped to `TargetInfo`.

9. **`internal/proxy/server.go` — add `TraefikTCPHosts` to `targetInfoEqual`**
   - Add `reflect.DeepEqual(a.TraefikTCPHosts, b.TraefikTCPHosts)` to the equality check
     so config reloads correctly detect TCP host changes.

10. **`main.go` — pass `TraefikTCPHosts` through snapshot**
    - Add `TraefikTCPHosts: s.TraefikTCPHosts` to the `TargetSnapshot` struct and populate
      it in `Snapshot()`.
    - Pass it through in the `/traefik` handler snapshot mapping.

11. **`internal/traefik/config_test.go` — revert REQ-081 tests, add REQ-082 tests**
    - Restore all existing tests to use `ListenPort` (undo per-container shape changes).
    - Remove REQ-081 TCP/UDP auto-generation tests.
    - Add new tests:
      - `TestBuildConfig_TCPSection_Single`: one `TraefikTCPHosts` entry → one TCP router+service.
      - `TestBuildConfig_TCPSection_HostSNIRule`: router rule is `HostSNI(\`domain\`)`.
      - `TestBuildConfig_TCPSection_UsesAddress`: service uses `address` not `url`.
      - `TestBuildConfig_TCPSection_PortMismatch`: entry port ≠ `ListenPort` → skipped.
      - `TestBuildConfig_TCPSection_NoEntries`: no `TraefikTCPHosts` → `tcp` absent from JSON.
      - `TestBuildConfig_TCPSection_WithCertResolver`: `tls.certResolver` set when resolver non-empty.
      - `TestBuildConfig_TCPAndHTTP_SameDomain`: same domain in both `TraefikHosts` and
        `TraefikTCPHosts` produces one HTTP entry and one TCP entry with no collision.
      - `TestBuildConfig_TCPSection_CustomProxyHost`: address uses custom proxy host.

12. **Update requirement and plan status, commit, push**

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/traefik/config.go` | Modify | Revert REQ-081 shape; add TCPConfig structs + TCP BuildConfig loop |
| `lazy-tcp-proxy/internal/traefik/config_test.go` | Modify | Revert REQ-081 tests; add REQ-082 TCP SNI tests |
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | Remove `UDPSnapshot()`; add `TraefikTCPHosts` to `targetInfoEqual`; populate in `Snapshot()` |
| `lazy-tcp-proxy/main.go` | Modify | Revert REQ-081 handler; pass `TraefikTCPHosts` through snapshot |
| `lazy-tcp-proxy/internal/types/types.go` | Modify | Add `TraefikTCPHosts []string` to `TargetInfo` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Parse `traefik-tcp-hosts` label in two locations |
| `lazy-tcp-proxy/internal/config/store.go` | Modify | Add `TraefikTCPHosts` to `ServiceEntry`; map to `TargetInfo` |

## API Contracts

`GET /traefik` — TCP section added alongside existing HTTP section when `traefik_tcp_hosts` set:

```json
{
  "http": {
    "routers": {
      "s3-example-com-9000-router": {
        "rule": "Host(`s3.example.com`)",
        "service": "s3-example-com-9000-service",
        "entryPoints": ["websecure"],
        "tls": { "certResolver": "myresolver" }
      }
    },
    "services": { "...": {} }
  },
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
        "loadBalancer": { "servers": [{ "address": "lazy-tcp-proxy:27015" }] }
      }
    }
  }
}
```

## Key Code Snippets

### `BuildConfig` TCP section

```go
tcpRouters := make(map[string]TCPRouter)
tcpServices := make(map[string]TCPService)

for _, snap := range snapshots {
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
        if entryPoint != "" {
            router.EntryPoints = []string{entryPoint}
        }
        if certResolver != "" {
            router.TLS = &RouterTLS{CertResolver: certResolver}
        }
        tcpRouters[name+"-router"] = router
        tcpServices[name+"-service"] = TCPService{
            LoadBalancer: TCPLoadBalancer{
                Servers: []TCPServer{{Address: fmt.Sprintf("%s:%d", proxyHost, port)}},
            },
        }
    }
}
if len(tcpRouters) > 0 {
    cfg.TCP = &TCPConfig{Routers: tcpRouters, Services: tcpServices}
}
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| TCP single entry | `TraefikTCPHosts:["mongo.example.com:27015"]`, `ListenPort:27015` | `tcp.routers["mongo-example-com-27015-router"]` with `HostSNI(\`mongo.example.com\`)` |
| TCP HostSNI rule | same | `rule` is `HostSNI(...)` not `Host(...)` |
| TCP uses address | same | service has `address: "lazy-tcp-proxy:27015"` not `url:` |
| TCP port mismatch | `TraefikTCPHosts:["mongo.example.com:9999"]`, `ListenPort:27015` | no TCP entry |
| TCP no entries | `TraefikTCPHosts:nil` | `tcp` key absent from JSON |
| TCP with certResolver | `certResolver:"myresolver"` | `tls.certResolver:"myresolver"` on router |
| TCP + HTTP same domain | both `TraefikHosts` and `TraefikTCPHosts` set | one entry in `http`, one in `tcp`, no collision |
| TCP custom proxy host | `proxyHost:"my-host"` | address `my-host:27015` |

## Risks & Open Questions

- No risk of name collision between HTTP and TCP router names: they live in separate
  `http.routers` and `tcp.routers` maps in the Traefik config.
- `TCPRouter` reuses `RouterTLS` (same struct as HTTP). Traefik's TCP TLS schema is identical
  to HTTP TLS for the `certResolver` field, so no separate struct is needed.
