# Fix: Config-Only Container Disappears After docker compose up — Implementation Plan

**Requirement**: [2026-05-18-fix-config-only-recreate.md](2026-05-18-fix-config-only-recreate.md)
**Date**: 2026-05-18
**Status**: Approved

## Implementation Steps

1. **`docker/manager.go` — `WatchEvents` `destroy` case: keep config-only targets registered**

   Change the config-only branch from `handler.RemoveTarget(targetID)` to
   `handler.ContainerStopped(targetID)`. The labeled-container path (`RemoveTarget`) is
   unchanged. Update the log message to reflect the new behaviour.

2. **`docker/manager.go` — `WatchEvents` `start` case: join new networks before marking running**

   In the config-only `start` branch, before calling `handler.ContainerStarted(rid)`:
   - Call `m.cli.ContainerInspect(ctx, msg.Actor.ID, ...)` to get the new container's network IDs.
   - Call `m.JoinNetworks(ctx, networkIDs)` so the proxy joins any fresh Docker network that
     `docker compose up` created.
   - Log newly joined networks (matching the style of the labeled-container path).

3. **Commit, push, update requirement to Completed**

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Two edits inside `WatchEvents`: destroy keeps config-only registered; start joins new networks |

## Key Code Snippets

### `destroy` case — before

```go
case "destroy":
    name := msg.Actor.Attributes["name"]
    targetID := msg.Actor.ID
    if rid := m.getConfigOnlyID(name); rid != "" {
        targetID = rid
        log.Printf("docker: event: config-only container removed: \033[33m%s\033[0m", name)
    } else {
        log.Printf("docker: event: container removed: \033[33m%s\033[0m", name)
    }
    handler.RemoveTarget(targetID)
```

### `destroy` case — after

```go
case "destroy":
    name := msg.Actor.Attributes["name"]
    if rid := m.getConfigOnlyID(name); rid != "" {
        // Config-only target: keep listeners; mark stopped so it recovers on restart.
        log.Printf("docker: event: config-only container removed: \033[33m%s\033[0m (kept registered, waiting for restart)", name)
        handler.ContainerStopped(rid)
    } else {
        log.Printf("docker: event: container removed: \033[33m%s\033[0m", name)
        handler.RemoveTarget(msg.Actor.ID)
    }
```

### `start` case for config-only — before

```go
if rid := m.getConfigOnlyID(name); rid != "" {
    if msg.Action == "start" {
        log.Printf("docker: event: config-only container started: \033[33m%s\033[0m", name)
        handler.ContainerStarted(rid)
    }
}
```

### `start` case for config-only — after

```go
if rid := m.getConfigOnlyID(name); rid != "" {
    if msg.Action == "start" {
        log.Printf("docker: event: config-only container started: \033[33m%s\033[0m", name)
        result, err := m.cli.ContainerInspect(ctx, msg.Actor.ID, client.ContainerInspectOptions{})
        if err == nil {
            var networkIDs []string
            for _, ep := range result.Container.NetworkSettings.Networks {
                if ep.NetworkID != "" {
                    networkIDs = append(networkIDs, ep.NetworkID)
                }
            }
            joined, err := m.JoinNetworks(ctx, networkIDs)
            if err != nil {
                log.Printf("docker: event: failed to join networks for config-only \033[33m%s\033[0m: %v", name, err)
            }
            for _, n := range joined {
                log.Printf("docker: event: joined network: \033[32m%s\033[0m", n)
            }
        }
        handler.ContainerStarted(rid)
    }
}
```

## Unit Tests

No new unit tests needed — the existing `WatchEvents` test coverage in `docker/manager_test.go`
exercises the event routing. The change is structural (which handler method is called), verified
by the compile check and existing tests.

## Risks & Open Questions

- **`destroy` followed by no `start`**: if a config-only container is genuinely removed and never
  recreated, the proxy keeps its listeners indefinitely until the next `discoverAndApply` (admin
  reload or proxy restart). This is acceptable — the same behaviour as labeled containers that are
  stopped (not destroyed).
- **Network inspect failure on `start`**: if `ContainerInspect` fails (e.g. timing race where
  container isn't fully initialised), `ContainerStarted` is still called so the target is marked
  running. The proxy will fall back to the "any network IP" path in `GetUpstreamHost`. Low risk.
