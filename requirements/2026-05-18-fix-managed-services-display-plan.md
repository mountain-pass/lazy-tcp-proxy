# Fix: Config-Only Services Always Show as Stopped — Implementation Plan

**Requirement**: [2026-05-18-fix-managed-services-display.md](2026-05-18-fix-managed-services-display.md)
**Date**: 2026-05-18
**Status**: Approved

## Root Cause Summary

Three compounding bugs prevent config-only containers from being managed:

1. **`handleConn` never sets `ts.running = true`** after `EnsureRunning` succeeds.
   For labeled containers this is papered over by the Docker "start" event, which calls
   `RegisterTarget(info{Running:true})`. Config-only containers never emit a proxied event,
   so `ts.running` stays `false` forever. Consequence: the idle-timeout checker sees
   `!ts.running → allIdle=false` and never stops the container; the dashboard always shows
   "stopped".

2. **`ContainerStarted` never sets `ts.running = true`**.
   `ContainerStarted` only handles dependant cascade; it doesn't update the running flag.
   For labeled containers this doesn't matter (RegisterTarget already set it). For config-only
   containers receiving a "start" event, calling `ContainerStarted` would be a no-op for
   the running flag.

3. **`WatchEvents` drops all events for containers without the `lazy-tcp-proxy.enabled=true`
   label**. Config-only containers never have this label. Additionally, `die`/`destroy` events
   use `msg.Actor.ID` (the real 64-char hex Docker ID) to call `ContainerStopped`/
   `RemoveTarget`, but config-only targets are registered with `ContainerID = name` (the
   Docker API accepts names wherever IDs are expected). So even the unfiltered `die` path
   silently misses config-only containers.

4. **Initial `Running` state is always `false` for YAML-only entries**.
   `config.Store.Apply()` creates a `TargetInfo` with `Running: false` (zero value) for
   containers that appear in the YAML but have no matching label-discovered entry.

## Implementation Steps

1. **`proxy/server.go` — `handleConn`: set `running=true` after `EnsureRunning` succeeds**
   Immediately after the `startGroup.Do` block returns with `startErr == nil`, acquire a
   write-lock and set `running = true` on every `targetState`/`udpListenerState` whose
   `ContainerID` matches. This ensures the inactivity checker can trigger idle stop, and the
   dashboard flips to idle/up on the first connection regardless of whether Docker events fire.

2. **`proxy/server.go` — `ContainerStarted`: also set `running=true`**
   Extend `ContainerStarted` to set `ts.running = true` / `uls.running = true` before
   cascading to dependants. This is idempotent for labeled containers (RegisterTarget already
   set it) and is the mechanism through which the event-watching fix (Step 6) updates state
   when a config-only container starts externally.

3. **`main.go` — extend `backendManager` interface**
   Add two methods:
   - `InspectRunning(ctx context.Context, targetID string) (bool, error)` — returns the
     container's actual running state; used at startup to populate YAML-only entries.
   - `SetConfigOnlyNames(nameToID map[string]string)` — hands the docker manager the
     name→registeredContainerID map so `WatchEvents` can look up config-only containers.

4. **`main.go` — `discoverAndApply`: populate running state + notify manager**
   After `store.Apply()` returns the merged slice:
   - Build a `discoveredIDSet` from `collector.Targets()` before Apply.
   - Iterate `merged`: for any entry whose `ContainerID` is **not** in `discoveredIDSet`
     (YAML-only entry), call `mgr.InspectRunning(ctx, t.ContainerID)` and write the result
     into `merged[i].Running`.
   - Build `configOnlyNameToID map[string]string` (name→ContainerID for each YAML-only
     entry) and call `mgr.SetConfigOnlyNames(configOnlyNameToID)`.
   - Both calls happen before `mgr.NotifyTargets(merged)` and `srv.Update(merged)`.

5. **`docker/manager.go` — `InspectRunning` + `SetConfigOnlyNames` + `getConfigOnlyID`**
   - Add `configOnlyIDs map[string]string` and `configOnlyMu sync.RWMutex` to the `Manager`
     struct (initialised to an empty map in `NewManager`).
   - `InspectRunning`: thin wrapper around `cli.ContainerInspect` returning
     `result.Container.State.Running`.
   - `SetConfigOnlyNames`: swaps the map under the write-lock.
   - `getConfigOnlyID`: reads the map under the read-lock; returns "" if not found.

6. **`docker/manager.go` — `WatchEvents`: handle config-only events**
   Modify the three event-case branches:
   - **`create`/`start`**: if the `lazy-tcp-proxy.enabled` label is absent, call
     `getConfigOnlyID(name)`. If the result is non-empty, log "config-only container
     started" and (for `start` action only) call `handler.ContainerStarted(registeredID)`.
     `continue` in either branch so the existing labeled-container path is unchanged.
   - **`die`**: resolve the effective target ID as
     `getConfigOnlyID(name)` if non-empty, else `msg.Actor.ID`. Call
     `handler.ContainerStopped(targetID)` with the resolved ID.
   - **`destroy`**: same resolution logic, call `handler.RemoveTarget(targetID)`.

7. **`k8s/backend.go` — stub implementations**
   Add `InspectRunning(_ context.Context, _ string) (bool, error) { return false, nil }` and
   `SetConfigOnlyNames(_ map[string]string) {}` to satisfy the updated `backendManager`
   interface. K8s events are label-filtered at the API level and `Running` state is already
   correct from `deploymentToTargetInfo`.

8. **Update + push requirement and plan files**

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | `handleConn`: set `running=true` after EnsureRunning; `ContainerStarted`: set `running=true` |
| `lazy-tcp-proxy/main.go` | Modify | Extend `backendManager` interface; update `discoverAndApply` to inspect + notify |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Add `InspectRunning`, `SetConfigOnlyNames`, `getConfigOnlyID`, `configOnlyIDs`/`configOnlyMu`; extend `WatchEvents` |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Add `InspectRunning` and `SetConfigOnlyNames` stubs |

## Key Code Snippets

### `handleConn` — set running after successful start (Step 1)

```go
_, startErr, shared := s.startGroup.Do(ts.info.ContainerID, func() (any, error) {
    return nil, s.backend.EnsureRunning(ctx, ts.info.ContainerID)
})
if shared {
    log.Printf("proxy: joined in-flight startup for \033[33m%s\033[0m", ts.info.ContainerName)
}
if startErr != nil {
    log.Printf("proxy: could not start container \033[33m%s\033[0m: %v", ts.info.ContainerName, startErr)
    return
}
// Mark container running so idle-timeout and dashboard reflect reality.
s.mu.Lock()
for _, t := range s.targets {
    if t.info.ContainerID == ts.info.ContainerID {
        t.running = true
    }
}
for _, u := range s.udpTargets {
    if u.info.ContainerID == ts.info.ContainerID {
        u.running = true
    }
}
s.mu.Unlock()
```

### `ContainerStarted` — set running (Step 2)

```go
func (s *ProxyServer) ContainerStarted(containerID string) {
    s.mu.Lock()
    var info types.TargetInfo
    for _, ts := range s.targets {
        if ts.info.ContainerID == containerID {
            ts.running = true
            info = ts.info
        }
    }
    for _, uls := range s.udpTargets {
        if uls.info.ContainerID == containerID {
            uls.running = true
        }
    }
    s.mu.Unlock()
    if len(info.Dependants) > 0 {
        go s.cascadeStart(info)
    }
}
```

### `discoverAndApply` — YAML-only running state + config-only map (Step 4)

```go
discovered := collector.Targets()
merged, errs := store.Apply(discovered, mgr.DefaultTargetID)
for _, e := range errs {
    log.Printf("config apply warning: %v", e)
}

discoveredIDSet := make(map[string]bool, len(discovered))
for _, t := range discovered {
    discoveredIDSet[t.ContainerID] = true
}

configOnlyNameToID := make(map[string]string)
for i, t := range merged {
    if !discoveredIDSet[t.ContainerID] {
        running, err := mgr.InspectRunning(ctx, t.ContainerID)
        if err != nil {
            log.Printf("config: could not inspect running state for %q: %v", t.ContainerName, err)
        } else {
            merged[i].Running = running
        }
        configOnlyNameToID[t.ContainerName] = t.ContainerID
    }
}
mgr.SetConfigOnlyNames(configOnlyNameToID)

mgr.NotifyTargets(merged)
// ... rest unchanged
```

### `WatchEvents` config-only handling for `die` (Step 6)

```go
case "die":
    name := msg.Actor.Attributes["name"]
    targetID := msg.Actor.ID
    if rid := m.getConfigOnlyID(name); rid != "" {
        targetID = rid
        log.Printf("docker: event: config-only container stopped: \033[33m%s\033[0m (still registered)", name)
    } else {
        log.Printf("docker: event: container stopped: \033[33m%s\033[0m (still registered)", name)
    }
    handler.ContainerStopped(targetID)
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `handleConn` sets running after start | Connection to stopped container; `EnsureRunning` returns nil | `ts.running == true` after call |
| `ContainerStarted` sets running | Call `ContainerStarted(id)` on a target with `running=false` | `ts.running == true`; cascade triggered if dependants |
| `discoverAndApply` inspects YAML-only | Collector returns no containers; config has 1 entry; InspectRunning returns true | `merged[0].Running == true`; `SetConfigOnlyNames` called with the entry |
| `WatchEvents` config-only start | `start` event for unlabeled container in configOnlyIDs | `ContainerStarted` called with registeredID |
| `WatchEvents` config-only die | `die` event for unlabeled container in configOnlyIDs | `ContainerStopped` called with registeredID (not Docker hex ID) |
| `WatchEvents` config-only destroy | `destroy` event for unlabeled container in configOnlyIDs | `RemoveTarget` called with registeredID |
| `WatchEvents` unknown unlabeled container | `start` event, not in configOnlyIDs | Logged as "not proxied"; no handler call |

## Risks & Open Questions

- **`ContainerStopped` uses `RLock` but writes to struct fields** — pre-existing data race, not introduced here; out of scope.
- **NetworkIDs remain empty for YAML-only targets** — the `GetUpstreamHost` fallback (any network IP) handles this; the hint optimisation is not fixed here.
- **Swarm config-only services** — `WatchServiceEvents` has a separate code path; the same pattern applies but is out of scope for this fix.
- **`configOnlyIDs` map replaced on each `discoverAndApply`** — safe because `SetConfigOnlyNames` does a full swap under the lock; any in-flight `WatchEvents` call using the old map will still produce a valid (possibly stale) result.
