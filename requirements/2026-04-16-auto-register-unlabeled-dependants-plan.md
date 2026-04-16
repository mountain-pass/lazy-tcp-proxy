# Make ports/udp-ports Labels Optional — Implementation Plan

**Requirement**: [2026-04-16-auto-register-unlabeled-dependants.md](2026-04-16-auto-register-unlabeled-dependants.md)
**Date**: 2026-04-16
**Status**: Implemented

---

## Implementation Steps

1. **`internal/docker/manager.go` — `containerToTargetInfo`: make ports optional**
   Remove the early error when neither `lazy-tcp-proxy.ports` nor
   `lazy-tcp-proxy.udp-ports` is present. Drop the `hasUDPPorts` variable
   (now redundant). Still error if `ports` is present but parses to zero
   mappings (user misconfiguration).

2. **`internal/docker/manager.go` — `WatchEvents`: relax port validation**
   Remove the early `continue` that skips containers missing port labels.
   Keep the format validation for when the `ports` label IS present but
   contains invalid tokens (restructure into an `if hasPorts { }` block).

3. **`internal/k8s/backend.go` — `deploymentToTargetInfo`: make ports optional**
   Same change as step 1 — remove the error when both port annotations are
   absent. Still error if `ports` annotation is present but parses to zero
   mappings.

4. **`internal/proxy/server.go` — add `dependantState` struct**
   Add a package-level struct to track running state for port-less containers:
   ```go
   type dependantState struct {
       containerName string
       running       bool
   }
   ```

5. **`internal/proxy/server.go` — add `dependantStates` field to `ProxyServer`**
   ```go
   dependantStates map[string]*dependantState // containerID → state (port-less only)
   ```
   Initialise in `NewServer`: `dependantStates: make(map[string]*dependantState)`.

6. **`internal/proxy/server.go` — `RegisterTarget`: populate `dependantStates`**
   After the existing `s.nameToID[info.ContainerName] = info.ContainerID` line,
   add a block that runs when `len(info.Ports) == 0 && len(info.UDPPorts) == 0`:
   - If entry already exists in `dependantStates`: update `containerName` and
     `running`; log `"proxy: updated target … (cascade-only, no ports)"`.
   - Otherwise: create new entry; log
     `"proxy: registered target … (cascade-only, no ports)"`.
   Port-bearing containers are unaffected (the new block is skipped).

7. **`internal/proxy/server.go` — `ContainerStopped`: update `dependantStates`**
   Inside the existing `s.mu.RLock()` section, after the `s.targets` and
   `s.udpTargets` loops, add:
   ```go
   if ds, ok := s.dependantStates[containerID]; ok {
       ds.running = false
   }
   ```

8. **`internal/proxy/server.go` — `ContainerStarted`: update `dependantStates`**
   Inside the existing `s.mu.RLock()` section, after the `s.targets` loop, add:
   ```go
   if ds, ok := s.dependantStates[containerID]; ok {
       ds.running = true
   }
   ```

9. **`internal/proxy/server.go` — `cascadeStop`: check and update `dependantStates`**
   In the first `s.mu.RLock()` block (where `running` is determined), after
   the existing `s.udpTargets` loop add:
   ```go
   if !running {
       if ds, ok := s.dependantStates[depID]; ok && ds.running {
           running = true
       }
   }
   ```
   In the second `s.mu.RLock()` block (after `StopContainer` succeeds), add:
   ```go
   if ds, ok := s.dependantStates[depID]; ok {
       ds.running = false
   }
   ```

10. **`internal/proxy/server.go` — `cascadeStart`: update `dependantStates`**
    In the `s.mu.RLock()` block that updates `running = true` after a
    successful `EnsureRunning`, add:
    ```go
    if ds, ok := s.dependantStates[depID]; ok {
        ds.running = true
    }
    ```

11. **`internal/proxy/server.go` — `RemoveTarget`: clean up `dependantStates`**
    After the existing `s.udpTargets` loop (before the `s.sched.Unregister`
    call), add:
    ```go
    if ds, ok := s.dependantStates[containerID]; ok {
        log.Printf("proxy: removing target \033[33m%s\033[0m (cascade-only)", ds.containerName)
        delete(s.nameToID, ds.containerName)
        delete(s.dependantStates, containerID)
    }
    ```

12. **`internal/proxy/server_test.go` — update `newTestServer`**
    Add `dependantStates: make(map[string]*dependantState)` to the struct
    literal in `newTestServer()`.

13. **`internal/proxy/server_test.go` — add cascade tests for port-less containers**
    Add `populateCascadeTargetsPortless` helper and two new test functions
    (see Unit Tests table below).

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/docker/manager.go` | Modify | `containerToTargetInfo`: remove ports-required error; `WatchEvents`: remove ports-required skip |
| `internal/k8s/backend.go` | Modify | `deploymentToTargetInfo`: remove ports-required error |
| `internal/proxy/server.go` | Modify | Add `dependantState` struct + `dependantStates` map; wire into `RegisterTarget`, `ContainerStopped`, `ContainerStarted`, `cascadeStop`, `cascadeStart`, `RemoveTarget` |
| `internal/proxy/server_test.go` | Modify | Update `newTestServer`; add port-less cascade tests |

---

## Key Code Snippets

### `containerToTargetInfo` — before (lines 154–169) vs after

```go
// BEFORE
portsStr, hasPorts := inspect.Config.Labels["lazy-tcp-proxy.ports"]
udpPortsStr, hasUDPPorts := inspect.Config.Labels["lazy-tcp-proxy.udp-ports"]
if !hasPorts && (!hasUDPPorts || udpPortsStr == "") {
    return types.TargetInfo{}, fmt.Errorf("missing label ...")
}
var ports []types.PortMapping
if hasPorts {
    ports = types.ParsePortMappings("lazy-tcp-proxy.ports", portsStr)
    if len(ports) == 0 {
        return types.TargetInfo{}, fmt.Errorf("label ... contains no valid port mappings")
    }
}
var udpPorts []types.PortMapping
if hasUDPPorts && udpPortsStr != "" {
    udpPorts = types.ParsePortMappings("lazy-tcp-proxy.udp-ports", udpPortsStr)
}

// AFTER
portsStr, hasPorts := inspect.Config.Labels["lazy-tcp-proxy.ports"]
udpPortsStr := inspect.Config.Labels["lazy-tcp-proxy.udp-ports"]
var ports []types.PortMapping
if hasPorts {
    ports = types.ParsePortMappings("lazy-tcp-proxy.ports", portsStr)
    if len(ports) == 0 {
        return types.TargetInfo{}, fmt.Errorf("label ... contains no valid port mappings")
    }
}
var udpPorts []types.PortMapping
if udpPortsStr != "" {
    udpPorts = types.ParsePortMappings("lazy-tcp-proxy.udp-ports", udpPortsStr)
}
```

### `WatchEvents` port validation — before vs after

```go
// BEFORE — the entire block is removed/restructured:
portsVal, hasPorts := attrs["lazy-tcp-proxy.ports"]
udpPortsVal := attrs["lazy-tcp-proxy.udp-ports"]
if !hasPorts && udpPortsVal == "" {
    log.Printf("... missing label ...")
    continue
}
valid := !hasPorts
if hasPorts {
    // ... loop ...
}
if !valid {
    log.Printf("... invalid ports value ...")
    continue
}

// AFTER — only validate when the label is actually present:
if portsVal, hasPorts := attrs["lazy-tcp-proxy.ports"]; hasPorts {
    valid := false
    for _, token := range strings.Split(portsVal, ",") {
        parts := strings.SplitN(strings.TrimSpace(token), ":", 2)
        if len(parts) == 2 {
            _, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
            _, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
            if e1 == nil && e2 == nil {
                valid = true
                break
            }
        }
    }
    if !valid {
        log.Printf("docker: event: container %s started but not proxied: invalid ports value %q", name, portsVal)
        continue
    }
}
```

### `RegisterTarget` — port-less block (inserted after `nameToID` update)

```go
if len(info.Ports) == 0 && len(info.UDPPorts) == 0 {
    if existing, ok := s.dependantStates[info.ContainerID]; ok {
        existing.containerName = info.ContainerName
        existing.running = info.Running
        log.Printf("proxy: updated target \033[33m%s\033[0m (cascade-only, no ports)", info.ContainerName)
    } else {
        s.dependantStates[info.ContainerID] = &dependantState{
            containerName: info.ContainerName,
            running:       info.Running,
        }
        log.Printf("proxy: registered target \033[33m%s\033[0m (cascade-only, no ports)", info.ContainerName)
    }
}
```

---

## Unit Tests

### New helper

```go
// populateCascadeTargetsPortless sets up hub (port-bearing) → ollama (port-less).
func populateCascadeTargetsPortless(s *ProxyServer, hubRunning, ollamaRunning bool) {
    hubInfo := types.TargetInfo{
        ContainerID:   "hub-id",
        ContainerName: "hub",
        Running:       hubRunning,
        Dependants:    []string{"ollama"},
    }
    s.targets[9000] = &targetState{
        info:       hubInfo,
        targetPort: 8080,
        running:    hubRunning,
        lastActive: time.Now().Add(-10 * time.Minute),
    }
    s.nameToID["hub"] = "hub-id"
    s.nameToID["ollama"] = "ollama-id"
    s.dependantStates["ollama-id"] = &dependantState{
        containerName: "ollama",
        running:       ollamaRunning,
    }
}
```

### New test cases

| Test | Input | Expected |
|------|-------|----------|
| `TestCascadeStop_StopsPortlessDependant` | hub running+idle, ollama port-less running; `checkInactivity` | `StopContainer("ollama-id")` called |
| `TestCascadeStop_SkipsAlreadyStoppedPortlessDependant` | hub running+idle, ollama port-less stopped; `checkInactivity` | no `StopContainer` call for ollama |

---

## Risks & Open Questions

- **`newTestServer` initialiser must include `dependantStates`** — any test
  that calls `RegisterTarget` with a port-less container will panic on nil map
  otherwise. Caught by adding the field to `newTestServer`.
- **No changes to `Snapshot` or the status dashboard** — port-less containers
  have no ports to display; leaving them out of the snapshot is intentional.
- **k8s `WatchEvents` watch filter is unchanged** — it already filters by
  label at the API server, so only labelled deployments (with or without port
  annotations) emit events. No second watch needed.
