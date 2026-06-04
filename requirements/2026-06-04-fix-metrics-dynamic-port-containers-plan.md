# Fix: /metrics Missing Containers with Only Dynamic Port Mappings — Implementation Plan

**Requirement**: [2026-06-04-fix-metrics-dynamic-port-containers.md](2026-06-04-fix-metrics-dynamic-port-containers.md)
**Date**: 2026-06-04
**Status**: Implemented

## Implementation Steps

1. **Add `AllocatePortMappings` to `PortAllocator`** (`internal/types/portalloc.go`)  
   New method that mirrors `AllocateForHosts` but also returns a `[]PortMapping` slice.
   Each non-empty-domain spec yields one `PortMapping{ListenPort: assigned, TargetPort: spec.TargetPort}`.
   Refactor `AllocateForHosts` to delegate to `AllocatePortMappings` (drop the `[]PortMapping` return value).

2. **Update `docker/manager.go` — `containerToTargetInfo`** (≈ line 267)  
   Replace two `AllocateForHosts` calls with `AllocatePortMappings`.
   Append returned `PortMapping` slices to `ports` (both HTTP and TCP-SNI traefik hosts produce TCP listeners).

3. **Update `docker/manager.go` — swarm service path** (≈ line 833)  
   Same replacement as step 2 for the swarm `inspectService` code path.

4. **Update `internal/k8s/backend.go`** (≈ line 362)  
   Same replacement: two `AllocateForHosts` calls → `AllocatePortMappings`, appending to `ports`.

5. **Update `internal/config/store.go` — `entryToTargetInfo`** (≈ line 275)  
   Same replacement: two `AllocateForHosts` calls → `AllocatePortMappings`, appending to `info.Ports`.

6. **Add unit tests for `AllocatePortMappings`** (`internal/types/portalloc_test.go`)  
   Cover: single traefik host, multiple hosts with different target ports, skipping claimed ports, stable re-allocation.

7. **Run `go test ./...` and `golangci-lint run`** from `lazy-tcp-proxy/` — fix any issues.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/portalloc.go` | Modify | Add `AllocatePortMappings`; refactor `AllocateForHosts` to delegate |
| `internal/docker/manager.go` | Modify | Two sites: replace `AllocateForHosts` → `AllocatePortMappings`, append to `ports` |
| `internal/k8s/backend.go` | Modify | One site: same replacement |
| `internal/config/store.go` | Modify | One site: same replacement |
| `internal/types/portalloc_test.go` | Modify | Add tests for `AllocatePortMappings` |

## Key Code Snippets

### New method on `PortAllocator`

```go
// AllocatePortMappings resolves a slice of TraefikHostSpec into
// "domain:listen_port" strings and corresponding PortMapping entries.
// Append the returned mappings to info.Ports so the proxy server creates
// TCP listeners for dynamically assigned ports.
func (a *PortAllocator) AllocatePortMappings(specs []TraefikHostSpec) ([]string, []PortMapping) {
    a.mu.Lock()
    defer a.mu.Unlock()
    hosts := make([]string, 0, len(specs))
    mappings := make([]PortMapping, 0, len(specs))
    for _, spec := range specs {
        if spec.Domain == "" {
            continue
        }
        var p int
        if existing, ok := a.assigned[spec.Domain]; ok {
            p = existing
        } else {
            p = a.nextFree()
            a.assigned[spec.Domain] = p
            a.claimed[p] = struct{}{}
        }
        hosts = append(hosts, fmt.Sprintf("%s:%d", spec.Domain, p))
        mappings = append(mappings, PortMapping{ListenPort: p, TargetPort: spec.TargetPort})
    }
    return hosts, mappings
}
```

### Updated `AllocateForHosts` (delegates)

```go
func (a *PortAllocator) AllocateForHosts(specs []TraefikHostSpec) []string {
    hosts, _ := a.AllocatePortMappings(specs)
    return hosts
}
```

### Call-site pattern (same at all three files)

```go
// Before:
traefikHosts = m.portAlloc.AllocateForHosts(traefikHostSpecs)
traefikTCPHosts = m.portAlloc.AllocateForHosts(traefikTCPHostSpecs)

// After:
var traefikPorts, traefikTCPPorts []types.PortMapping
traefikHosts, traefikPorts = m.portAlloc.AllocatePortMappings(traefikHostSpecs)
traefikTCPHosts, traefikTCPPorts = m.portAlloc.AllocatePortMappings(traefikTCPHostSpecs)
ports = append(ports, traefikPorts...)
ports = append(ports, traefikTCPPorts...)
```

> **Note**: In `config/store.go`, the field is `info.Ports` rather than a local `ports` variable — the append targets `info.Ports` directly.

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| Single spec | `{Domain:"a.com", TargetPort:80}`, base 8000 | hosts=`["a.com:8000"]`, mappings=`[{8000,80}]` |
| Two specs, different target ports | `{a.com:80}`, `{b.com:443}`, base 8000 | hosts=`["a.com:8000","b.com:8001"]`, mappings=`[{8000,80},{8001,443}]` |
| Claimed port skipped | claim 8000, spec `{a.com:80}`, base 8000 | hosts=`["a.com:8001"]`, mappings=`[{8001,80}]` |
| Stable re-allocation | call twice with same spec | same port both times |
| Empty domain skipped | `{Domain:"", TargetPort:80}` | hosts=`[]`, mappings=`[]` |

## Risks & Open Questions

- **Duplicate PortMapping check**: if the same listen port appears in both explicit `ports` and a traefik spec (theoretically possible if `ClaimPorts` wasn't called first), there would be two entries for the same `ListenPort`. The existing `ClaimPorts` call before `AllocatePortMappings` prevents this — the allocator skips claimed ports — so no additional dedup is needed.
- **`AllocateForHosts` callers**: the method is used in traefik config tests (`internal/traefik/config_test.go`) via the `Snapshot` type, not through `PortAllocator`. No change needed there.
