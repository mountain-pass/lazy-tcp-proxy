# Include Stopped Containers' Allocated Memory in /status

**Date Added**: 2026-06-06
**Priority**: Medium
**Status**: Planned

## Problem Statement

The `containers` array in the `/status` response (added by REQ-109) only includes **running** containers, because it is built from `ContainerStats`, which only returns live usage data for running containers. Operators want to see the configured/allocated memory for **stopped** containers too, so they can plan capacity even when a lazily-started service is currently idle.

## Functional Requirements

1. The `containers` array in `GET /status` MUST include stopped (non-running) Docker containers in addition to running ones.
2. Each entry MUST gain a `running` boolean field indicating whether the container is currently running.
3. For stopped containers:
   - `memory_used` MUST be `0` (no live usage data is available).
   - `memory_limit` MUST reflect the container's configured memory limit (`HostConfig.Memory` from `ContainerInspect`), or `0` if no limit is configured (consistent with the existing "0 = unlimited" convention for running containers).
4. For running containers, behaviour is unchanged from REQ-109 (`memory_used`/`memory_limit` sourced from `ContainerStats`).
5. The aggregate `memory_used` / `memory_total` fields MUST remain unchanged (i.e. still computed only from running containers).

## User Experience Requirements

- The Svelte dashboard's per-container memory column (REQ-110) should be able to show stopped containers with their allocated memory and a visual indicator that they're not currently running (e.g. "stopped" badge or grey row), while showing `memory_used` as `0`/`–`.

## Technical Requirements

- Extend `ContainerMemoryStats` in `internal/docker/manager.go` to list **all** containers (`ContainerListOptions{All: true}`) instead of only running ones.
- For containers whose `State` is not `"running"`, skip the `ContainerStats` call (it returns no useful data for stopped containers) and instead call `ContainerInspect` to read `HostConfig.Memory` as `memory_limit`, with `memory_used` set to `0`.
- Add a `Running bool` field (JSON: `running`) to `types.ContainerMemoryStat`.
- Keep the existing aggregate `used`/`total` computation scoped to running containers only — do not add stopped containers' (zero) usage to the aggregate.
- The Kubernetes backend stub continues to return `nil` for `containers`.

## Acceptance Criteria

- [ ] `GET /status` `containers` array includes both running and stopped containers.
- [ ] Each entry has `name`, `memory_used`, `memory_limit`, and `running` fields.
- [ ] Stopped container entries report `memory_used: 0` and `memory_limit` equal to their configured Docker memory limit (or `0` if unlimited).
- [ ] Running container entries are unaffected (same values as before this change).
- [ ] Aggregate `memory_used` / `memory_total` values are unchanged (computed from running containers only).
- [ ] `go build ./...`, `golangci-lint run`, and `go test ./...` pass.

## Dependencies

- REQ-109: Per-Container Memory Stats in /status Endpoint — this requirement extends that work.
- REQ-110: Dashboard Per-Container Memory Column — may need a follow-up to render the new `running` field.

## Implementation Notes

- `ContainerList(ctx, client.ContainerListOptions{All: true})` returns stopped containers too; use `c.State` (`container.ContainerState`, e.g. `"running"`, `"exited"`, `"created"`) to branch behaviour.
- `ContainerInspect` returns `result.Container.HostConfig.Memory` (an `int64`, in bytes; `0` means unlimited) — this is the same value Docker reports as the container's configured memory limit, equivalent to what `ContainerStats`'s `MemoryStats.Limit` reflects for running containers.
