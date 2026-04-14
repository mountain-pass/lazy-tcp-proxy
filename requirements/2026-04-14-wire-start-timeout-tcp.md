# Wire START_TIMEOUT_SECS to TCP Dial Retry Loop

**Date Added**: 2026-04-14
**Priority**: Medium
**Status**: Completed

## Problem Statement

`START_TIMEOUT_SECS` and the per-container `lazy-tcp-proxy.start-timeout-secs`
label were introduced in REQ-061 to control how long the proxy waits for a UDP
upstream to become ready after a cold start. The TCP connection path has an
equivalent retry loop (`handleConn` in `server.go`) but still uses hardcoded
constants (`dialRetries = 30`, `dialInterval = 1s`), making the TCP cold-start
budget unconfigurable and inconsistent with UDP.

## Functional Requirements

1. The TCP dial retry loop MUST use `START_TIMEOUT_SECS` (global default 30 s)
   as its timeout budget, with the same per-container
   `lazy-tcp-proxy.start-timeout-secs` label override that UDP already supports.
2. `dialInterval` (1 s between dial attempts) remains a fixed internal constant;
   only the total budget (`dialRetries`) is derived from the configured timeout.

## Technical Requirements

- `targetState` gains a `startTimeout time.Duration` field, populated in
  `RegisterTarget` using `effectiveTimeout(info.StartTimeout, s.startTimeout)`.
- `handleConn` derives `retries` from `ts.startTimeout` at the start of the
  dial loop:
  ```go
  retries := int((ts.startTimeout + dialInterval - 1) / dialInterval)
  if retries < 1 { retries = 1 }
  ```
- The `dialRetries` constant is removed; `dialInterval` is kept.
- No change to `TargetInfo`, `types.go`, `main.go`, the backends, the README,
  or any label parsing — all of that already exists from REQ-061.

## Acceptance Criteria

- [x] `targetState` has a `startTimeout time.Duration` field.
- [x] TCP dial retry count is derived from `ts.startTimeout` (not the removed
      `dialRetries` constant).
- [x] A container with `lazy-tcp-proxy.start-timeout-secs=10` uses ~10 s of TCP
      dial retries (10 attempts × 1 s).
- [x] Existing unit and integration tests pass.

## Dependencies

- REQ-061 (Fix UDP Cold-Start Timeouts) — provides `TargetInfo.StartTimeout`,
  label parsing, `ProxyServer.startTimeout`, and `effectiveTimeout` reuse.
- `internal/proxy/server.go` — sole file of concern.
