# Cron-Only Container Discovery

**Date Added**: 2026-06-11
**Priority**: Medium
**Status**: Completed

## Problem Statement

Containers that use `lazy-tcp-proxy.cron-start` to schedule periodic batch jobs (e.g. rclone sync) but expose no network ports are silently skipped during discovery with a confusing error message. The log message also uses the short container ID instead of the human-readable container name, making it hard to identify which container is affected.

## Functional Requirements

1. A container with `lazy-tcp-proxy.cron-start` set should be discovered and registered even if it has no port labels (`lazy-tcp-proxy.ports`, `lazy-tcp-proxy.udp-ports`, `lazy-tcp-proxy.traefik-hosts`, `lazy-tcp-proxy.traefik-tcp-hosts`).
2. The "skipping container" log message must display the container name (in yellow ANSI colour) instead of the short container ID.

## User Experience Requirements

- Users running cron-only batch containers (no exposed ports) can use `lazy-tcp-proxy.cron-start` without also supplying a dummy port label.
- Log output identifies the skipped container by name so operators can quickly locate the offending compose service.

## Technical Requirements

- Modify the port-label guard in `containerToTargetInfo` (`internal/docker/manager.go`) to also accept `lazy-tcp-proxy.cron-start` as a sufficient label.
- Update the error message text to include `lazy-tcp-proxy.cron-start` in the list of accepted labels.
- In the `Discover` loop, resolve the container name from `c.Names[0]` (trimming the leading `/`) and wrap it in `\033[33m…\033[0m` (yellow) in the log line.

## Acceptance Criteria

- [ ] A container with only `lazy-tcp-proxy.enabled=true` and `lazy-tcp-proxy.cron-start=…` is registered (not skipped) during discovery.
- [ ] The skip log message reads `skipping container <name>:` with the name in yellow, not the short container ID.
- [ ] The error message text lists `lazy-tcp-proxy.cron-start` as one of the accepted labels.
- [ ] `go build ./internal/...` passes with no errors.

## Dependencies

- Relates to: [2026-04-06-cron-scheduling.md](2026-04-06-cron-scheduling.md)

## Implementation Notes

- `c.Names` is populated by `ContainerList`; the first entry is always `/name`. Falls back to `c.ID[:12]` if the slice is empty.
- The service-level equivalent (`serviceToTargetInfo`) was not changed — Swarm services don't use `cron-start`.
