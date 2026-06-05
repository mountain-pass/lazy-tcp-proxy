# Per-Container Memory Stats in /status Endpoint

**Date Added**: 2026-06-05
**Priority**: Medium
**Status**: Planned

## Problem Statement

The `/status` endpoint currently exposes only aggregate memory figures (`memory_used`, `memory_total`) that sum across all running containers. There is no way to tell which container is consuming how much memory. Operators want per-container memory visibility in the same JSON payload, sourced from the Docker socket.

## Functional Requirements

1. The `/status` JSON response MUST include a `containers` array alongside the existing `services`, `memory_used`, and `memory_total` fields.
2. Each element in `containers` represents one running Docker container visible to the Docker socket and contains:
   - `name` — the container name (first name, stripped of leading `/`)
   - `memory_used` — active memory in bytes (RSS-like: `Usage` minus `inactive_file` cache, same formula already used for aggregate `memory_used`)
   - `memory_limit` — the container's configured memory limit in bytes, or the host's total memory if no limit is set (i.e. when the Docker-reported limit equals the host total or 0)
3. If Docker is unavailable or the stats call fails, the `containers` field MUST be omitted (same nil-pointer pattern used for `memory_used`/`memory_total`).
4. On the Kubernetes backend, `containers` MUST be omitted (same no-op pattern as the existing `MemoryStats`).

## User Experience Requirements

- The `/status` JSON is consumed by both the Svelte dashboard and external tooling. Adding a new top-level `containers` key is backwards-compatible — existing consumers that ignore unknown keys are unaffected.
- The dashboard MAY show per-container memory later; this requirement covers only the API shape.

## Technical Requirements

- Reuse the existing `ContainerStats` calls already made inside `MemoryStats`. The implementation should NOT make a second round of `ContainerStats` calls — instead a new method `ContainerMemoryStats` should replace or wrap `MemoryStats` to return both aggregate and per-container data in one pass.
- The `backendManager` interface in `main.go` gains a new method `ContainerMemoryStats(ctx) (used, total int64, containers []ContainerMemoryStat, err error)` where `ContainerMemoryStat` is a small struct `{Name string, MemoryUsed int64, MemoryLimit int64}`.
- The existing `MemoryStats` method is kept on the interface for backwards compatibility but delegates to `ContainerMemoryStats` internally (or is removed if no other callers exist — check before deciding).
- The Kubernetes backend stub returns `nil` for the containers slice.
- The Docker socket is already mounted; no new configuration is required.

## Acceptance Criteria

- [ ] `GET /status` response includes a `containers` array when Docker is available.
- [ ] Each container entry has `name`, `memory_used`, and `memory_limit` fields (integers, bytes).
- [ ] `memory_limit` reflects the container's Docker memory limit, or host total RAM when no limit is configured.
- [ ] `memory_used` per container uses the same `Usage - inactive_file` formula as the existing aggregate.
- [ ] `containers` is absent (not `null`) from the response when Docker stats are unavailable.
- [ ] The existing `memory_used` and `memory_total` aggregate fields remain unchanged.
- [ ] No additional Docker API calls are made beyond those already needed for the aggregate stats.
- [ ] `go build ./...` and `golangci-lint run` pass with no new violations.
- [ ] `go test ./...` passes.

## Dependencies

- REQ-025: HTTP Status Endpoint (List Managed Containers) — adds to this endpoint
- REQ-095/REQ-105: Rename /status → /metrics → /status history — endpoint is currently `/status`

## Implementation Notes

- `ContainerList` + `ContainerStats` fan-out is already implemented in `MemoryStats` (manager.go:1157). The new method extends this to capture per-container data in the same goroutine fan-out.
- Container names from the Docker API are of the form `["/name"]`; strip the leading `/` for the JSON output.
- `MemoryStats.Limit` from the stats response gives the container memory limit; when this equals `MemTotal` from `docker info` (or is 0), treat it as "no limit" and use host total instead — or simply always expose the raw Docker limit and let callers interpret 0 as unlimited.
