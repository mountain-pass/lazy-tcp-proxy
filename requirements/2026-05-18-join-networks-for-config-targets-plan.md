# Auto-Join Docker Networks for Static Config Targets — Implementation Plan

**Requirement**: [2026-05-18-join-networks-for-config-targets.md](2026-05-18-join-networks-for-config-targets.md)
**Date**: 2026-05-18
**Status**: Implemented

## Implementation Steps

1. **`main.go`** — Add `JoinNetworksForContainerNames` to the `backendManager` interface.
2. **`internal/docker/manager.go`** — Implement `JoinNetworksForContainerNames`: inspect each container by name, extract network IDs, call `JoinNetworks`.
3. **`internal/k8s/backend.go`** — Add no-op implementation of `JoinNetworksForContainerNames`.
4. **`main.go` / `discoverAndApply()`** — After `store.Apply()`, collect names of merged targets with empty `NetworkIDs` and call `mgr.JoinNetworksForContainerNames()`.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/main.go` | Modify | Add method to `backendManager` interface; call it in `discoverAndApply()` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Add `JoinNetworksForContainerNames` implementation |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Add no-op `JoinNetworksForContainerNames` |

## Key Code Snippets

### Docker manager implementation

```go
func (m *Manager) JoinNetworksForContainerNames(ctx context.Context, names []string) {
    for _, name := range names {
        result, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
        if err != nil {
            // Container doesn't exist yet — silent skip
            continue
        }
        var networkIDs []string
        for _, ep := range result.Container.NetworkSettings.Networks {
            if ep.NetworkID != "" {
                networkIDs = append(networkIDs, ep.NetworkID)
            }
        }
        if _, err := m.JoinNetworks(ctx, networkIDs); err != nil {
            log.Printf("docker: failed to join networks for %s: %v", name, err)
        }
    }
}
```

### K8s no-op

```go
func (b *Backend) JoinNetworksForContainerNames(_ context.Context, _ []string) {}
```

### discoverAndApply addition

```go
var configOnlyNames []string
for _, t := range merged {
    if len(t.NetworkIDs) == 0 {
        configOnlyNames = append(configOnlyNames, t.ContainerName)
    }
}
mgr.JoinNetworksForContainerNames(ctx, configOnlyNames)
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| Container running, in one network | name resolves, NetworkSettings has one network ID | `JoinNetworks` called with that ID |
| Container stopped, networks present | same — Docker preserves NetworkSettings for stopped containers | `JoinNetworks` called with that ID |
| Container not found | `ContainerInspect` returns 404 | silent skip, no log, no error |
| Container has no networks | NetworkSettings.Networks empty | `JoinNetworks` called with empty slice (no-op) |
| K8s backend | any names | no-op, compiles cleanly |

## Risks & Open Questions

- `JoinNetworks` already handles the "already connected" case idempotently, so calling it on a reload for a network the proxy is already in is safe.
- If a container's networks change between calls (e.g. recreated with different networks), the proxy accumulates memberships but does not leave stale ones — consistent with existing behaviour for label-discovered containers.
