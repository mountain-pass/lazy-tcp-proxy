# Fix: Stale Docker Network Error on Container Start — Implementation Plan

**Requirement**: [2026-04-11-fix-stale-network-error.md](2026-04-11-fix-stale-network-error.md)
**Date**: 2026-04-11
**Status**: Implemented

## Implementation Steps

1. **Add `warnSharedDefaultNetworks()` helper** in `manager.go`:
   - Signature: `func (m *Manager) warnSharedDefaultNetworks(ctx context.Context, info types.TargetInfo)`
   - Early-return if `m.selfID == ""`
   - For each `networkID` in `info.NetworkIDs`:
     - Call `m.cli.NetworkInspect(ctx, networkID, client.NetworkInspectOptions{})`
     - Skip on error (non-fatal)
     - If `netInfo.Network.Labels["com.docker.compose.network"] != "default"` → skip
     - Check if `m.selfID` appears as a key prefix in `netInfo.Network.Containers`
     - If proxy IS a member → log the warning (see UX requirements for exact text)

2. **Call `warnSharedDefaultNetworks()` in `Discover()`** — after `containerToTargetInfo`
   succeeds, before `JoinNetworks`, so the warning fires even when already on the
   same network:
   ```go
   info, err := m.containerToTargetInfo(ctx, c.ID)
   // ...
   m.warnSharedDefaultNetworks(ctx, info)
   joined, err := m.JoinNetworks(ctx, info.NetworkIDs)
   ```

3. **Call `warnSharedDefaultNetworks()` in `WatchEvents()`** — in the `"create"/"start"`
   branch, after `containerToTargetInfo` succeeds:
   ```go
   info, err := m.containerToTargetInfo(ctx, msg.Actor.ID)
   // ...
   m.warnSharedDefaultNetworks(ctx, info)
   joined, err := m.JoinNetworks(ctx, info.NetworkIDs)
   ```

4. **Add stale-network hint in `EnsureRunning()`** — after `ContainerStart` returns an
   error, before returning:
   ```go
   if _, err := m.cli.ContainerStart(...); err != nil {
       if strings.Contains(err.Error(), "network") && strings.Contains(err.Error(), "not found") {
           log.Printf("docker: container %q has a stale network reference; recreate the container to fix this (docker rm %s && docker compose up -d)", name, name)
       }
       return fmt.Errorf("starting container: %w", err)
   }
   ```

5. **Run `go test ./...`** to confirm all tests pass.

6. **Update REQ-056 status** to Completed in the requirement file and index.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Add `warnSharedDefaultNetworks()`, call it from `Discover()` and `WatchEvents()`, add hint in `EnsureRunning()` |
| `requirements/2026-04-11-fix-stale-network-error.md` | Modify | Status → Completed |
| `requirements/_index.md` | Modify | Status → Completed |

## Key Code Snippets

### `warnSharedDefaultNetworks()`

```go
func (m *Manager) warnSharedDefaultNetworks(ctx context.Context, info types.TargetInfo) {
    if m.selfID == "" {
        return
    }
    for _, netID := range info.NetworkIDs {
        netInfo, err := m.cli.NetworkInspect(ctx, netID, client.NetworkInspectOptions{})
        if err != nil {
            continue
        }
        if netInfo.Network.Labels["com.docker.compose.network"] != "default" {
            continue
        }
        for cid := range netInfo.Network.Containers {
            if strings.HasPrefix(cid, m.selfID) || strings.HasPrefix(m.selfID, cid) {
                log.Printf("docker: WARNING: container %q shares the proxy's default compose network %q.\n"+
                    "  Running \"docker compose down\" on the proxy stack will delete this network and leave %q unable to restart.\n"+
                    "  Fix: add a top-level \"name:\" field to each of your compose files to give them unique project names.",
                    info.ContainerName, netInfo.Network.Name, info.ContainerName)
                break
            }
        }
    }
}
```

## Unit Tests

No new test cases are added — the existing integration tests cover the `Discover` and
`EnsureRunning` paths. The warning and hint are log-only and do not alter control flow,
so they do not require dedicated unit tests.

## Risks & Open Questions

- The `com.docker.compose.network=default` label is a Docker Compose implementation
  detail. It has been stable across Compose v1 and v2 but is not part of a formal API
  contract. If Compose changes this label in a future version the warning would silently
  stop firing — acceptable as the feature is best-effort guidance.
