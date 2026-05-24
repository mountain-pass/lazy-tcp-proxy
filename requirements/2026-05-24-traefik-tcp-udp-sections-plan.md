# Traefik TCP and UDP Provider Sections — Implementation Plan

**Requirement**: [2026-05-24-traefik-tcp-udp-sections.md](2026-05-24-traefik-tcp-udp-sections.md)
**Date**: 2026-05-24
**Status**: Implemented

## Implementation Steps

1. **`internal/traefik/config.go` — reshape `Snapshot` to per-container**
   - Remove `ListenPort int` field.
   - Add `ContainerName string`, `TCPPorts []int`, `UDPPorts []int`.
   - Keep `TraefikHosts []string` unchanged.
   - Update `BuildConfig` HTTP section: replace `port != snap.ListenPort` guard with
     `!containsInt(snap.TCPPorts, port)` helper so HTTP entries are still only emitted for
     ports that are actually registered.

2. **`internal/traefik/config.go` — add TCP structs and generation**
   - Add `TCPConfig`, `TCPRouter`, `TCPService`, `TCPLoadBalancer`, `TCPServer` structs.
   - Add `TCP *TCPConfig \`json:"tcp,omitempty"\`` to `TraefikConfig`.
   - In `BuildConfig`, for each `snap.TCPPorts` entry emit one `TCPRouter` + `TCPService`.
     - Router name: `sanitiseName("<containerName>-tcp-<port>") + "-router"`
     - Service name: same prefix + `"-service"`
     - Rule: `"HostSNI(\`*\`)"` (catch-all; no domain discrimination)
     - EntryPoints: `["tcp-<port>"]`
     - Server address: `"<proxyHost>:<port>"` (note: TCP/UDP use `address`, not `url`)
   - If no TCP entries were added, leave `TraefikConfig.TCP` nil (omitted from JSON).

3. **`internal/traefik/config.go` — add UDP structs and generation**
   - Add `UDPConfig`, `UDPRouter`, `UDPService`, `UDPLoadBalancer`, `UDPServer` structs.
   - Add `UDP *UDPConfig \`json:"udp,omitempty"\`` to `TraefikConfig`.
   - In `BuildConfig`, for each `snap.UDPPorts` entry emit one `UDPRouter` + `UDPService`.
     - Router name: `sanitiseName("<containerName>-udp-<port>") + "-router"`
     - Service name: same prefix + `"-service"`
     - EntryPoints: `["udp-<port>"]` (no Rule field — UDP routers have none)
     - Server address: `"<proxyHost>:<port>"`
   - If no UDP entries were added, leave `TraefikConfig.UDP` nil (omitted from JSON).

4. **`internal/proxy/server.go` — add `UDPSnapshot()` method**
   - Mirror `Snapshot()` but iterate `s.udpTargets` instead of `s.targets`.
   - Return `[]TargetSnapshot` (same struct, reused). `TraefikHosts` will be empty/nil
     for UDP snapshots — that is fine; the field is only used for HTTP routing.

5. **`main.go` — update `/traefik` handler to build per-container snapshots**
   - Call both `srv.Snapshot()` (TCP) and `srv.UDPSnapshot()` (UDP).
   - Group by `ContainerName` into a local map, collecting `TCPPorts`, `UDPPorts`, and
     `TraefikHosts` (from TCP snapshots; same per container so first-seen is sufficient).
   - Build `[]traefikpkg.Snapshot` from the grouped map and pass to `BuildConfig`.

6. **`internal/traefik/config_test.go` — update existing tests**
   - Replace `ListenPort: 9001` with `TCPPorts: []int{9001}` (and add `ContainerName`).
   - All existing test assertions remain the same — only the input struct changes.

7. **`internal/traefik/config_test.go` — add new TCP tests**
   - `TestBuildConfig_TCPSection_Single`: one TCP port → one router+service in `tcp` section.
   - `TestBuildConfig_TCPSection_Multiple`: two TCP ports → two router+service pairs.
   - `TestBuildConfig_TCPSection_NoTCPPorts`: no TCP ports → `tcp` key absent from output.
   - `TestBuildConfig_TCPSection_CustomProxyHost`: `TRAEFIK_PROXY_HOST` appears in address.

8. **`internal/traefik/config_test.go` — add new UDP tests**
   - `TestBuildConfig_UDPSection_Single`: one UDP port → one router+service in `udp` section.
   - `TestBuildConfig_UDPSection_Multiple`: two UDP ports → two router+service pairs.
   - `TestBuildConfig_UDPSection_NoUDPPorts`: no UDP ports → `udp` key absent from output.
   - `TestBuildConfig_UDPSection_NoRule`: UDP router must have no `rule` field (verified via
     JSON marshalling — `rule` key absent).
   - `TestBuildConfig_TCPAndUDP_SamePort`: same port number in both TCPPorts and UDPPorts
     produces one TCP entry and one UDP entry with correct entrypoint names.

9. **Update requirement and plan status, commit, push**

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/traefik/config.go` | Modify | Reshape Snapshot; add TCP+UDP structs and generation in BuildConfig |
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | Add `UDPSnapshot()` method |
| `lazy-tcp-proxy/main.go` | Modify | Update `/traefik` handler to group TCP+UDP by container |
| `lazy-tcp-proxy/internal/traefik/config_test.go` | Modify | Update existing tests; add TCP+UDP test cases |

## API Contracts

No new HTTP endpoints. `/traefik` response extended:

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

When `tcp` or `udp` sections are empty they are **absent** from the response (pointer + `omitempty`).

## Data Models

### Updated `traefik.Snapshot` (per-container, not per-port)

```go
type Snapshot struct {
    ContainerName string   // used as name prefix in router/service names
    TCPPorts      []int    // all TCP listen ports for this container
    UDPPorts      []int    // all UDP listen ports for this container
    TraefikHosts  []string // unchanged — domain:port pairs for HTTP section
}
```

### New structs in `internal/traefik/config.go`

```go
type TCPConfig struct {
    Routers  map[string]TCPRouter  `json:"routers"`
    Services map[string]TCPService `json:"services"`
}
type TCPRouter struct {
    EntryPoints []string `json:"entryPoints"`
    Rule        string   `json:"rule"`
    Service     string   `json:"service"`
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

type UDPConfig struct {
    Routers  map[string]UDPRouter  `json:"routers"`
    Services map[string]UDPService `json:"services"`
}
type UDPRouter struct {
    EntryPoints []string `json:"entryPoints"`
    Service     string   `json:"service"`
    // No Rule field — Traefik UDP routers have no rule
}
type UDPService struct {
    LoadBalancer UDPLoadBalancer `json:"loadBalancer"`
}
type UDPLoadBalancer struct {
    Servers []UDPServer `json:"servers"`
}
type UDPServer struct {
    Address string `json:"address"`
}
```

### Updated `TraefikConfig`

```go
type TraefikConfig struct {
    HTTP HTTPConfig  `json:"http"`
    TCP  *TCPConfig  `json:"tcp,omitempty"`
    UDP  *UDPConfig  `json:"udp,omitempty"`
}
```

## Key Code Snippets

### `BuildConfig` TCP generation (inside existing loop)

```go
// TCP section
tcpRouters := make(map[string]TCPRouter)
tcpServices := make(map[string]TCPService)
for _, snap := range snapshots {
    prefix := sanitiseName(snap.ContainerName)
    for _, port := range snap.TCPPorts {
        name := fmt.Sprintf("%s-tcp-%d", prefix, port)
        tcpRouters[name+"-router"] = TCPRouter{
            EntryPoints: []string{fmt.Sprintf("tcp-%d", port)},
            Rule:        "HostSNI(`*`)",
            Service:     name + "-service",
        }
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

### `/traefik` handler grouping in `main.go`

```go
raw := srv.Snapshot()
rawUDP := srv.UDPSnapshot()

type cEntry struct {
    tcpPorts     []int
    udpPorts     []int
    traefikHosts []string
}
byName := make(map[string]*cEntry)
for _, s := range raw {
    e := byName[s.ContainerName]
    if e == nil {
        e = &cEntry{traefikHosts: s.TraefikHosts}
        byName[s.ContainerName] = e
    }
    e.tcpPorts = append(e.tcpPorts, s.ListenPort)
}
for _, s := range rawUDP {
    e := byName[s.ContainerName]
    if e == nil {
        e = &cEntry{}
        byName[s.ContainerName] = e
    }
    e.udpPorts = append(e.udpPorts, s.ListenPort)
}
snaps := make([]traefikpkg.Snapshot, 0, len(byName))
for name, e := range byName {
    snaps = append(snaps, traefikpkg.Snapshot{
        ContainerName: name,
        TCPPorts:      e.tcpPorts,
        UDPPorts:      e.udpPorts,
        TraefikHosts:  e.traefikHosts,
    })
}
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| TCP single port | `TCPPorts:[5432]`, name `postgres` | `tcp.routers["postgres-tcp-5432-router"]` with `rule=HostSNI(\`*\`)`, entryPoints `["tcp-5432"]` |
| TCP multiple ports | `TCPPorts:[5432,5433]` | Two router+service pairs |
| TCP no ports | `TCPPorts:nil` | `tcp` key absent from JSON |
| TCP custom host | `TCPPorts:[5432]`, host `my-host` | address `my-host:5432` |
| UDP single port | `UDPPorts:[53]`, name `dns` | `udp.routers["dns-udp-53-router"]` with entryPoints `["udp-53"]` and **no rule field** |
| UDP multiple ports | `UDPPorts:[53,5353]` | Two router+service pairs |
| UDP no ports | `UDPPorts:nil` | `udp` key absent from JSON |
| Same port TCP+UDP | `TCPPorts:[53]`, `UDPPorts:[53]` | Both `tcp-53` and `udp-53` entries present |
| Existing HTTP test | `TCPPorts:[9001]`, `TraefikHosts:["whoami.localhost:9001"]` | HTTP section unchanged |

## Risks & Open Questions

- The `Snapshot` type in `traefik` package changes shape (removing `ListenPort`). All callers
  are in `main.go` and `config_test.go` — both will be updated in the same PR.
- `UDPSnapshot()` does not include `TraefikHosts` (UDP targets don't have that field). This is
  correct — `TraefikHosts` is only relevant for HTTP routing.
- Traefik TCP `HostSNI('*')` requires the entrypoint to NOT have TLS configured in static config
  (or to use passthrough mode). This is a user-side concern; document in the example if one is
  added.
