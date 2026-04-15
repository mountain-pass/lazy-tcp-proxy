# Docker HEALTHCHECK Readiness Gate — Implementation Plan

**Requirement**: [2026-04-15-docker-healthcheck-readiness.md](2026-04-15-docker-healthcheck-readiness.md)
**Date**: 2026-04-15
**Status**: Implemented

## Implementation Steps

1. **`internal/types/types.go`** — add `HasHealthCheck bool` to `TargetInfo`.

2. **`internal/docker/manager.go`** — in `containerToTargetInfo`, populate
   `HasHealthCheck` from `inspect.State.Health`:
   `Health != nil && Health.Status != "" && Health.Status != "none"`.
   Add `WaitUntilHealthy` method on `Manager`: polls `ContainerInspect →
   State.Health.Status` every `dialInterval` until `"healthy"`, returns error
   on `"unhealthy"` or timeout.

3. **`internal/k8s/backend.go`** — add `WaitUntilHealthy` as an immediate
   `return nil` no-op (Kubernetes readiness is managed by the cluster).

4. **`internal/proxy/server.go`**:
   a. Add `WaitUntilHealthy` to the `containerBackend` interface.
   b. Add `hasHealthCheck bool` field to `targetState`.
   c. Populate it in `RegisterTarget` (TCP path) from `info.HasHealthCheck`.
   d. In `handleConn`, after the existing `http-healthcheck` block, add:
      if `ts.hasHealthCheck && ts.httpHealthCheck == ""` → call
      `s.backend.WaitUntilHealthy(...)`.

5. **`internal/proxy/server_test.go`** — add `WaitUntilHealthy` to
   `mockBackend` (no-op). Existing tests unaffected.

6. **`internal/docker/manager_test.go`** — add unit tests for
   `WaitUntilHealthy`: immediate healthy, starting→healthy, unhealthy terminal,
   timeout, no healthcheck (none).

7. **`README.md`** — add a note in the Container Label Configuration section
   and a new sub-section under HTTP Health Check describing the automatic
   Docker HEALTHCHECK behaviour.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | Add `HasHealthCheck bool` to `TargetInfo` |
| `internal/docker/manager.go` | Modify | Populate `HasHealthCheck`; add `WaitUntilHealthy` |
| `internal/k8s/backend.go` | Modify | Add no-op `WaitUntilHealthy` |
| `internal/proxy/server.go` | Modify | Add to interface, `targetState`, `RegisterTarget`, `handleConn` |
| `internal/proxy/server_test.go` | Modify | Add `WaitUntilHealthy` to `mockBackend` |
| `internal/docker/manager_test.go` | Modify | Unit tests for `WaitUntilHealthy` |
| `README.md` | Modify | Document the automatic Docker HEALTHCHECK behaviour |

## Key Code Snippets

### `types.go` — updated `TargetInfo`

```go
HasHealthCheck   bool           // true if the container has a HEALTHCHECK configured
```

### `docker/manager.go` — `HasHealthCheck` detection

```go
hasHealthCheck := inspect.State.Health != nil &&
    inspect.State.Health.Status != "" &&
    inspect.State.Health.Status != "none"
```

### `docker/manager.go` — `WaitUntilHealthy`

```go
func (m *Manager) WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error {
    retries := int((timeout + dialInterval - 1) / dialInterval)
    if retries < 1 {
        retries = 1
    }
    for attempt := 1; attempt <= retries; attempt++ {
        result, err := m.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
        if err != nil {
            return fmt.Errorf("inspecting container: %w", err)
        }
        status := ""
        if result.Container.State.Health != nil {
            status = result.Container.State.Health.Status
        }
        switch status {
        case "healthy":
            log.Printf("proxy: docker-healthcheck: \033[33m%s\033[0m healthy", name)
            return nil
        case "unhealthy":
            return fmt.Errorf("container \033[33m%s\033[0m is unhealthy", name)
        default:
            log.Printf("proxy: docker-healthcheck: attempt %d: \033[33m%s\033[0m → %s", attempt, name, status)
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(dialInterval):
        }
    }
    return fmt.Errorf("container \033[33m%s\033[0m not healthy after %s", name, timeout)
}
```

### `k8s/backend.go` — no-op

```go
func (b *Backend) WaitUntilHealthy(_ context.Context, _, _ string, _ time.Duration) error {
    return nil
}
```

### `proxy/server.go` — updated interface

```go
type containerBackend interface {
    EnsureRunning(ctx context.Context, targetID string) error
    StopContainer(ctx context.Context, targetID, targetName string) error
    GetUpstreamHost(ctx context.Context, targetID, hint string) (string, error)
    WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error
}
```

### `proxy/server.go` — call site in `handleConn`

```go
// After the existing http-healthcheck block:
if ts.hasHealthCheck && ts.httpHealthCheck == "" {
    if err := s.backend.WaitUntilHealthy(ctx, ts.info.ContainerID, ts.info.ContainerName, ts.startTimeout); err != nil {
        log.Printf("proxy: docker-healthcheck: %v; dropping connection from \033[36m%s\033[0m", err, conn.RemoteAddr())
        return
    }
}
```

## Unit Tests

| Test | Input | Expected |
|------|-------|----------|
| `WaitUntilHealthy` — immediate healthy | health status = `"healthy"` on first inspect | returns nil after 1 attempt |
| `WaitUntilHealthy` — starting then healthy | `"starting"` × 2 then `"healthy"` | returns nil after 3 attempts |
| `WaitUntilHealthy` — unhealthy terminal | health status = `"unhealthy"` | returns error immediately |
| `WaitUntilHealthy` — timeout | always `"starting"`, timeout = 2 s | returns error |
| `WaitUntilHealthy` — ctx cancelled | ctx cancelled before first inspect | returns ctx.Err() |
| `HasHealthCheck` detection — present | `Health.Status = "starting"` | `HasHealthCheck = true` |
| `HasHealthCheck` detection — none | `Health.Status = "none"` | `HasHealthCheck = false` |
| `HasHealthCheck` detection — nil | `Health = nil` | `HasHealthCheck = false` |

## Risks & Open Questions

- `ContainerInspect` is called once per second during cold start. This is
  intentional and low-cost (local Unix socket call).
- The `dialInterval` constant (1 s) is defined in `server.go` but needed in
  `docker/manager.go`. To avoid a circular import, the constant will be
  duplicated as a package-local constant in `manager.go`
  (`const healthCheckPollInterval = time.Second`), keeping the packages
  independent.
