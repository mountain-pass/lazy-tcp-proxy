# Fix: Wake During a Cascade Stop Leaves Dependants Stopped

**Date Added**: 2026-10-21
**Priority**: High
**Status**: Completed

## Problem Statement

When an upstream with `dependants` is idle-stopped, `cascadeStop` stops the dependants one by one in a goroutine. If a connection wakes the upstream while that cascade is still running, the wake's `cascadeStart` runs concurrently with it:

- a dependant that is in the middle of `docker stop` still inspects as Running, so `EnsureRunning` returns without starting it, and the stop then completes;
- dependants later in the list are still running when `cascadeStart` checks them, and `cascadeStop` stops them afterwards.

The upstream ends up running with its dependants stopped (e.g. Nextcloud without MariaDB/Redis), and nothing recovers it: clients keep connecting, so the upstream never goes idle and no new cascade start happens. Seen in production with a Nextcloud stack whose MariaDB/Redis were dependants; a graceful stop of a dependant can take ~10 s (seen in the logs for an ERPNext stack), which makes the window easy to hit with sync clients.

## Functional Requirements

1. A cascade start and a cascade stop of the same upstream never run at the same time; a cascade start that arrives during a cascade stop waits for it and then starts every dependant that is stopped.
2. A cascade stop stops no further dependants once the upstream is running again.
3. No change for upstreams without dependants, or when no wake happens during a cascade.

## Technical Requirements

- Per-upstream `*sync.Mutex` (keyed by upstream ContainerID, `sync.Map` on `ProxyServer`) held for the whole of `cascadeStart` / `cascadeStop`.
- `cascadeStop` checks `isRunning(upstream)` before each dependant and returns early (logged) if it is running.

## Acceptance Criteria

- [x] A cascade start during a blocked dependant stop does not call `EnsureRunning` until the stop returns, then starts the dependant.
- [x] A cascade stop whose upstream becomes running mid-cascade leaves the remaining dependants running.
- [x] Both new tests fail on the previous code and pass with the fix (`-race`).
- [x] `golangci-lint run` and `go test ./...` pass.

## Dependencies

- Cascade start/stop (`dependants`).

## Implementation Notes

- Design and plan collapsed into this single lightweight document (bug fix submitted as an external PR).
- Files: `internal/proxy/server.go` (`cascadeLocks`, `cascadeLock()`, lock + abort check in `cascadeStop`, lock in `cascadeStart`), `internal/proxy/server_test.go` (`TestCascadeStart_WaitsForInFlightCascadeStop`, `TestCascadeStop_AbortsWhenUpstreamRunningAgain`).
- A wake during a cascade stop now waits for the dependant being stopped (at most its stop timeout) before the dependants are started again; the upstream itself is not delayed.
