# Status Dashboard — ⚠️ Icon for Missing/Removed Containers

**Date Added**: 2026-05-29
**Priority**: Medium
**Status**: Completed

## Problem Statement

`docker system prune` removes stopped containers. Because `lazy-tcp-proxy` stops idle containers, managed containers are often stopped and therefore vulnerable to being pruned. When a config-only container (registered via `config.yaml`) is pruned, the proxy keeps the listener alive — but the status dashboard previously showed 🔴 (stopped) for both a cleanly-stopped container and a destroyed/missing one, giving no indication that the container no longer exists.

## Functional Requirements

- The status dashboard must display ⚠️ when a container has been removed from Docker (destroyed) but is still registered in the proxy.
- The distinction must be preserved in the `/status` JSON API via a `container_missing` boolean field.
- When the container is recreated and comes back online, the ⚠️ icon must revert to 🟢 or 🟠 automatically.
- Icon legend:
  - 🟢 — running, with active connections
  - 🟠 — running, idle
  - 🔴 — stopped (container exists but is not running)
  - ⚠️ — missing (container has been removed/pruned)

## User Experience Requirements

- The ⚠️ icon must appear in the Target column of the status dashboard table.
- No user action is required for the icon to update — it reflects live state from the `/status` endpoint, which polls every 2 seconds.

## Technical Requirements

- Add a `ContainerRemoved(containerID string)` method to the `TargetHandler` interface (`internal/types/types.go`).
- Add a `missing bool` field to `targetState` (`internal/proxy/server.go`) and `udpListenerState` (`internal/proxy/udp.go`).
- Add `ContainerMissing bool` (JSON: `container_missing`) to `TargetSnapshot`.
- Implement `ContainerRemoved()` on `ProxyServer`: sets `running = false` and `missing = true`; also resets UDP upstream readiness state.
- `ContainerStarted()` must clear `missing = false` when a container comes back.
- Docker manager: on `destroy` events for config-only containers, call `handler.ContainerRemoved()` instead of `handler.ContainerStopped()`.
- Add no-op `ContainerRemoved` implementations to `config/store.go` and the k8s test mock (`internal/k8s/backend_test.go`).
- Frontend `statusIcon()`: check `snap.container_missing` first; return `'⚠️'` before checking `running`.

## Acceptance Criteria

- [x] `/status` JSON includes `container_missing: true` after `docker system prune` removes a config-only container
- [x] Status dashboard shows ⚠️ for a missing container
- [x] Status dashboard shows 🔴 for a stopped (but not missing) container
- [x] ⚠️ reverts to 🟢/🟠 automatically when the container is recreated
- [x] All existing tests pass

## Dependencies

- REQ-080 — Fix: Config-Only Container Disappears After docker compose up (established the config-only container lifecycle pattern this builds on)

## Implementation Notes

Changes span 7 files: `main.go`, `internal/types/types.go`, `internal/proxy/server.go`, `internal/proxy/udp.go`, `internal/docker/manager.go`, `internal/config/store.go`, `internal/k8s/backend_test.go`. Design, Plan, and Build phases were combined in a single step at user direction since the scope was well-defined and unambiguous.
