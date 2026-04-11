# Fix: Stale Docker Network Error on Container Start

**Date Added**: 2026-04-11
**Priority**: High
**Status**: Planned

## Problem Statement

When a Docker container is started by lazy-tcp-proxy and its configured network has been
deleted (e.g. after `docker-compose down` removed the network but the container still
exists), Docker returns:

```
Error response from daemon: failed to set up container networking: network <SHA> not found
```

The proxy propagates this as `"starting container: <error>"`, giving the user no
indication of the root cause or how to fix it.

## Functional Requirements

1. When `ContainerStart` fails with a "network … not found" error, the proxy MUST log an
   additional actionable hint message that:
   - Identifies the problem (stale network reference in the container config)
   - Tells the user how to fix it (recreate the container)
2. The proxy continues to return the original error to callers so existing error-handling
   behaviour is unchanged.

## User Experience Requirements

- The operator should immediately understand the problem without having to consult Docker
  documentation.
- Example log output:
  ```
  docker: container pihole has a stale network reference (<SHA>); recreate the container to fix this (e.g. docker rm pihole && docker-compose up -d pihole)
  ```

## Technical Requirements

- Detection: check whether the error string from `ContainerStart` matches the pattern
  `"network"` AND `"not found"` (case-insensitive substring check is sufficient; no
  regex required).
- Change is limited to `EnsureRunning()` in
  `lazy-tcp-proxy/internal/docker/manager.go`.
- Do NOT change the returned error — only add a `log.Printf` hint before returning.

## Acceptance Criteria

- [ ] When `ContainerStart` returns an error containing both "network" and "not found",
      a hint log line is emitted that mentions "stale network" and "recreate".
- [ ] When `ContainerStart` fails for any other reason, no hint log is emitted.
- [ ] When `ContainerStart` succeeds, behaviour is unchanged.
- [ ] `go test ./...` continues to pass.

## Dependencies

- REQ-001 (Core TCP Proxy for Docker Containers) — this is a UX improvement to the
  existing container-start flow.

## Implementation Notes

- The check can reuse the existing `strings` import already present in `manager.go`.
- No new dependencies are required.
