# Per-Container and Overall Disk Usage Stats in /status Endpoint

**Date Added**: 2026-06-06
**Priority**: Medium
**Status**: Planned

## Problem Statement

The `/status` endpoint currently exposes memory usage (per-container and overall, via REQ-109/REQ-095) sourced from the Docker socket, but no disk usage information. Operators want to see how much disk space containers and the overall Docker host are consuming, to help spot runaway log files, bloated images, or storage pressure — the same way they can already spot memory pressure.

## Functional Requirements

1. The `/status` JSON response MUST include overall disk usage figures (e.g. `disk_used`, `disk_total`) sourced from the Docker `/system/df` API (images + containers + volumes + build cache vs. host filesystem capacity), mirroring the existing `memory_used`/`memory_total` aggregate fields.
2. Each element in the existing `containers` array (REQ-109) MUST additionally include per-container disk usage:
   - `disk_used` — the container's writable layer size (`SizeRw`)
   - `disk_total` — the container's total size including its image (`SizeRootFs`)
3. If fetching disk usage is unavailable, errors, or is too slow (see Technical Requirements), the disk fields MUST be omitted — same nil-pointer/omission pattern already used for memory fields. The endpoint must never block or fail because disk stats could not be retrieved.
4. On the Kubernetes backend, the new disk fields MUST be omitted (same no-op pattern as existing memory stats).

## User Experience Requirements

- The `/status` JSON is consumed by both the Svelte dashboard and external tooling. Adding new keys is backwards-compatible.
- The dashboard MUST show a disk usage column/value alongside the existing memory column (mirroring REQ-110), both per-container and as an overall summary.
- Values are displayed in human-readable units (e.g. MB/GB), consistent with how memory is currently formatted on the dashboard.

## Technical Requirements

- **Overall usage**: one call to Docker's `GET /system/df` (Go client: `cli.DiskUsage(ctx, ...)`), analogous to the existing `Info()` call in `ContainerMemoryStats` (manager.go:~1161). Requires Docker API ≥ v1.25 (already satisfied by the current client version).
- **Per-container usage**: pass `size=true` to the existing `ContainerList` call (manager.go:~1167) to receive `SizeRw`/`SizeRootFs` directly — no extra round trip per container.
- **Performance constraint**: `size=true` makes Docker walk each container's overlay filesystem, which can be slow on hosts with many containers or large writable layers. This MUST be measured during the Plan phase. If it regularly exceeds ~3 seconds (e.g. on a representative dev/test host), the implementation must either:
  - cache/refresh disk sizes on a slower interval than the rest of `/status`, or
  - apply a timeout and omit per-container disk fields when the call is slow, falling back gracefully (per FR3).
- Reuse the existing fan-out / goroutine patterns in `ContainerMemoryStats` where practical; avoid duplicating Docker API plumbing.
- The Docker socket is already mounted; no new configuration is required.

## Acceptance Criteria

- [ ] `GET /status` response includes overall `disk_used`/`disk_total` fields when Docker is available and the call completes promptly.
- [ ] Each entry in `containers` includes `disk_used` and `disk_total` fields (bytes) when available.
- [ ] All new fields are omitted (not erroring, not blocking) when Docker is unavailable, the call fails, or it is too slow.
- [ ] The dashboard displays overall and per-container disk usage, formatted consistently with the existing memory display.
- [ ] No regression to existing `memory_*` fields or other `/status` content.
- [ ] `go build ./...`, `golangci-lint run`, and `go test ./...` all pass with no new violations.

## Dependencies

- REQ-109: Per-Container Memory Stats in /status Endpoint — this requirement extends the same `containers` array
- REQ-110: Dashboard Per-Container Memory Column — disk column mirrors this UI pattern
- REQ-105: Rename /metrics → /status — endpoint is currently `/status`

## Implementation Notes

- Mirrors the structure of REQ-109 (backend) + REQ-110 (frontend), but for disk instead of memory.
- The Plan phase must include a measurement of `ContainerList(..., size=true)` latency on a representative host/container count before committing to the "always include" vs. "cached/throttled" approach.
