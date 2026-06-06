# Overall "/" Root Drive Disk Usage in /status Endpoint

**Date Added**: 2026-06-06
**Priority**: Medium
**Status**: In Progress

## Problem Statement

The `/status` endpoint currently exposes overall memory usage (`memory_used`/`memory_total`, via REQ-095) but no disk usage information. Operators want to see at a glance how much of the root (`/`) drive is used vs. total, displayed at the top of the dashboard alongside the existing memory summary, to help spot storage pressure.

## Functional Requirements

1. The `/status` JSON response MUST include overall disk usage figures (`disk_used`, `disk_total`) for the root (`/`) filesystem only — i.e. used vs. total bytes of the drive the proxy runs on (equivalent to `df /`), mirroring the existing `memory_used`/`memory_total` aggregate fields.
2. If fetching disk usage is unavailable or errors, the disk fields MUST be omitted — same nil-pointer/omission pattern already used for memory fields. The endpoint must never block or fail because disk stats could not be retrieved.
3. Per-container disk usage is explicitly OUT OF SCOPE for this requirement.

## User Experience Requirements

- The `/status` JSON is consumed by both the Svelte dashboard and external tooling. Adding new top-level keys is backwards-compatible.
- The dashboard MUST show overall disk used vs. total at the top of the page, alongside the existing overall memory used vs. total summary.
- Values are displayed in human-readable units (e.g. MB/GB), consistent with how memory is currently formatted on the dashboard.

## Technical Requirements

- Obtained via a `statfs("/")` syscall (e.g. `golang.org/x/sys/unix.Statfs` or `syscall.Statfs`), computing `disk_total = Blocks * Bsize` and `disk_used = (Blocks - Bavail) * Bsize`. This is local/instant (no Docker API round trip) and works whether or not Docker is reachable.
- No new Docker API calls or socket access required — this is independent of the Docker/Kubernetes backend.
- Reuse the existing nil-pointer/omission pattern for optional stats fields (as used for `memory_used`/`memory_total`).

## Acceptance Criteria

- [ ] `GET /status` response includes overall `disk_used`/`disk_total` fields reflecting the root (`/`) filesystem's used vs. total bytes (via `statfs`).
- [ ] Fields are omitted (not erroring, not blocking) if the `statfs` call fails.
- [ ] The dashboard displays overall disk used vs. total at the top of the page, formatted consistently with the existing memory display.
- [ ] No regression to existing `memory_*` fields or other `/status` content.
- [ ] `go build ./...`, `golangci-lint run`, and `go test ./...` all pass with no new violations.

## Dependencies

- REQ-095: Rename /status → /metrics and Add Memory Fields — overall disk fields mirror the overall memory fields added here
- REQ-105: Rename /metrics → /status — endpoint is currently `/status`

## Implementation Notes

- Simpler than originally scoped: no Docker API interaction needed, since this only reports the root filesystem of the host/container the proxy runs on, via a direct `statfs` syscall.
