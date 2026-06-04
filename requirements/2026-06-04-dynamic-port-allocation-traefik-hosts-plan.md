# Dynamic Port Allocation for traefik_hosts / traefik_tcp_hosts — Implementation Plan

**Requirement**: [2026-06-04-dynamic-port-allocation-traefik-hosts.md](2026-06-04-dynamic-port-allocation-traefik-hosts.md)
**Date**: 2026-06-04
**Status**: Approved

## Implementation Steps

1. **Add `PortAllocator` to `internal/types/portalloc.go`** — new file, process-global allocator that tracks claimed ports and assigns the next free one starting from a configurable base.

2. **Update `ParseTraefikHosts` in `internal/types/types.go`** — change the comment and log message: the port suffix is now the *target* port (integer validation stays identical, but semantics change). Rename function to `ParseTraefikHostSpecs` to make the break explicit, accepting `domain:target_port` entries and returning `[]TraefikHostSpec` (struct with `Domain string` + `TargetPort int`).

3. **Update `internal/docker/manager.go` — `containerToTargetInfo`** — after parsing traefik host specs, call the allocator to resolve `domain:listen_port` pairs. Update validation: accept the container if any one of `ports`, `udp-ports`, `traefik_hosts`, `traefik_tcp_hosts` is present and valid.

4. **Update `internal/docker/manager.go` — `inspectService`** — same validation and allocation changes as step 3.

5. **Update `internal/docker/manager.go` — `WatchEvents` start-event handler** — update the early-out validation check (lines ~1049–1072) to also pass if `traefik_hosts` or `traefik_tcp_hosts` attributes are non-empty.

6. **Update `internal/k8s/backend.go` — pod-to-target conversion** — same validation relaxation (accept if any of ports/udp-ports/traefik_hosts/traefik_tcp_hosts present). Add allocation call for traefik host specs (k8s backend uses annotations with the same key names).

7. **Update `internal/config/store.go` — `entryToTargetInfo`** — change the validation from "ports or udp_ports required" to "ports, udp_ports, traefik_hosts, or traefik_tcp_hosts required". Add allocation call for traefik host specs parsed from the YAML entry. Update the placeholder comment in `Load()` to show the new `domain:target_port` format.

8. **Wire `PortAllocator` into `main.go`** — read `LISTEN_START_PORT` env var (default `8000`), construct the allocator, pass it to docker manager and config store. Log the configured start port at startup.

9. **Update `internal/types/types_test.go`** — update/add tests for the renamed `ParseTraefikHostSpecs` function and add allocator unit tests.

10. **Update `internal/config/store_test.go`** (if it exists) and `internal/docker/manager_test.go` — update tests affected by the validation change.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/portalloc.go` | Create | `PortAllocator` struct; `Claim`, `AllocateForHosts` methods |
| `internal/types/types.go` | Modify | Rename `ParseTraefikHosts` → `ParseTraefikHostSpecs`; new `TraefikHostSpec` struct; update comment |
| `internal/docker/manager.go` | Modify | Use `ParseTraefikHostSpecs` + allocator; relax validation in 3 places |
| `internal/k8s/backend.go` | Modify | Relax validation; use allocator for traefik host specs |
| `internal/config/store.go` | Modify | `entryToTargetInfo` validation; allocator call; placeholder comment |
| `main.go` | Modify | Read `LISTEN_START_PORT`; construct and inject allocator |
| `internal/types/types_test.go` | Modify | Update `ParseTraefikHosts` tests; add allocator tests |

---

## Data Models

### New `TraefikHostSpec` (in `internal/types/portalloc.go` or `types.go`)

```go
// TraefikHostSpec is a parsed traefik_hosts / traefik_tcp_hosts entry.
// Domain is the hostname; TargetPort is the container's listening port.
type TraefikHostSpec struct {
    Domain     string
    TargetPort int
}
```

### `PortAllocator`

```go
// PortAllocator assigns sequential listen ports to traefik host entries.
// It tracks all claimed ports (from explicit mappings and prior allocations)
// and skips them when assigning new ones.
type PortAllocator struct {
    mu        sync.Mutex
    base      int         // LISTEN_START_PORT
    claimed   map[int]struct{}
    assigned  map[string]int  // domain → already-assigned listen port (stable across re-registrations)
}

// ClaimPorts marks the given ports as taken (from explicit lazy-tcp-proxy.ports labels).
func (a *PortAllocator) ClaimPorts(ports []PortMapping)

// AllocateForHosts resolves a slice of TraefikHostSpec into domain:listen_port strings,
// assigning new ports deterministically. Already-assigned domains reuse their port.
// Returns the resolved []string in the same format as TargetInfo.TraefikHosts.
func (a *PortAllocator) AllocateForHosts(specs []TraefikHostSpec) []string
```

---

## Key Code Snippets

### `PortAllocator.AllocateForHosts`

```go
func (a *PortAllocator) AllocateForHosts(specs []TraefikHostSpec) []string {
    a.mu.Lock()
    defer a.mu.Unlock()
    out := make([]string, 0, len(specs))
    for _, spec := range specs {
        key := spec.Domain
        if p, ok := a.assigned[key]; ok {
            out = append(out, fmt.Sprintf("%s:%d", key, p))
            continue
        }
        p := a.nextFree()
        a.assigned[key] = p
        a.claimed[p] = struct{}{}
        out = append(out, fmt.Sprintf("%s:%d", key, p))
    }
    return out
}

func (a *PortAllocator) nextFree() int {
    p := a.base
    for {
        if _, taken := a.claimed[p]; !taken {
            return p
        }
        p++
    }
}
```

### Validation change in `containerToTargetInfo` (docker)

```go
traefikHostSpecs := types.ParseTraefikHostSpecs("lazy-tcp-proxy.traefik-hosts", ...)
traefikTCPHostSpecs := types.ParseTraefikHostSpecs("lazy-tcp-proxy.traefik-tcp-hosts", ...)

// Old: !hasPorts && (!hasUDPPorts || udpPortsStr == "")
// New:
if !hasPorts && (!hasUDPPorts || udpPortsStr == "") &&
    len(traefikHostSpecs) == 0 && len(traefikTCPHostSpecs) == 0 {
    return types.TargetInfo{}, fmt.Errorf("missing required label: one of lazy-tcp-proxy.ports, lazy-tcp-proxy.udp-ports, lazy-tcp-proxy.traefik-hosts, lazy-tcp-proxy.traefik-tcp-hosts")
}

// Pre-claim explicit ports so allocator skips them
alloc.ClaimPorts(ports)
alloc.ClaimPorts(udpPorts)

traefikHosts := alloc.AllocateForHosts(traefikHostSpecs)
traefikTCPHosts := alloc.AllocateForHosts(traefikTCPHostSpecs)
```

### `WatchEvents` early-out relaxation

```go
// Before calling containerToTargetInfo, the event handler currently checks
// attrs["lazy-tcp-proxy.ports"] and attrs["lazy-tcp-proxy.udp-ports"].
// Add: also pass if traefik-hosts or traefik-tcp-hosts is non-empty.
traefikHostsVal := attrs["lazy-tcp-proxy.traefik-hosts"]
traefikTCPHostsVal := attrs["lazy-tcp-proxy.traefik-tcp-hosts"]
if !hasPorts && udpPortsVal == "" && traefikHostsVal == "" && traefikTCPHostsVal == "" {
    log.Printf("docker: event: container %s started but not proxied: ...")
    continue
}
```

### Injecting the allocator

The allocator is a process-global singleton constructed in `main.go` and passed to the docker manager and config store. Since the docker Manager and config Store are the only callers of `containerToTargetInfo`, `inspectService`, and `entryToTargetInfo`, passing the allocator as a constructor parameter (or a setter method) is the cleanest approach.

```go
// main.go
listenStartPort := resolveListenStartPort() // reads LISTEN_START_PORT, default 8000
alloc := types.NewPortAllocator(listenStartPort)
dockerMgr.SetPortAllocator(alloc)
cfgStore.SetPortAllocator(alloc)
```

---

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestPortAllocator_BasicAllocation` | specs `[{s3.example.com, 9000}]`, base 8000 | `["s3.example.com:8000"]` |
| `TestPortAllocator_TwoHosts` | specs `[{a.com, 80}, {b.com, 443}]`, base 8000 | `["a.com:8000", "b.com:8001"]` |
| `TestPortAllocator_SkipsClaimedPort` | claim port 8000, then allocate `[{a.com, 80}]` | `["a.com:8001"]` |
| `TestPortAllocator_StableReassignment` | allocate `[{a.com, 80}]` twice | same port returned both times |
| `TestPortAllocator_ExplicitPortsNotOverlap` | `ClaimPorts([{8000, 9000}])`, allocate `[{b.com, 9000}]` | `["b.com:8001"]` |
| `TestParseTraefikHostSpecs_Valid` | `"s3.example.com:9000,mongo.example.com:27017"` | `[{s3.example.com, 9000}, {mongo.example.com, 27017}]` |
| `TestParseTraefikHostSpecs_InvalidPort` | `"bad.host:notanumber"` | empty (skipped with warning) |
| `TestValidation_TraefikHostsOnly` | container with only `traefik-hosts` label | accepted (no error) |
| `TestValidation_NoneOfTheAbove` | container with none of the 4 labels | rejected with clear error |

---

## Risks & Open Questions

- The allocator is shared between docker discovery (which runs sorted containers in batch) and the event handler (which processes containers one at a time as they start). For event-driven additions, the "next free" logic will simply append after whatever is already allocated — determinism only applies to the batch discovery phase on startup. This is acceptable per the design.
- The k8s backend does not currently have traefik host parsing at all (the grep showed no `TraefikHosts` assignment in `k8s/backend.go`). The validation relaxation still applies, but the allocation call is a no-op until someone adds traefik labels to k8s annotations. The plan covers this minimally (add the relaxation; skip allocation if there are no specs).
- `config/store.go`'s `entryToTargetInfo` already assigns `TraefikHosts = entry.TraefikHosts` verbatim (the YAML stores `domain:listen_port` strings currently). After this change, the YAML format also changes to `domain:target_port`, and the allocator resolves them before storing in `TargetInfo`. Existing YAML configs with the old `domain:listen_port` format will get double-port-forwarded to wrong ports — this is a breaking change to config.yaml format. This is acceptable and should be noted in release notes.
