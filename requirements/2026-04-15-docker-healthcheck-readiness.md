# Docker HEALTHCHECK Readiness Gate

**Date Added**: 2026-04-15
**Priority**: Medium
**Status**: Completed

## Problem Statement

REQ-063 added an optional `lazy-tcp-proxy.http-healthcheck` label that polls a
URL before forwarding TCP traffic. However, many popular images (postgres,
mysql, redis, etc.) already ship with a Docker `HEALTHCHECK` instruction that
signals exactly the same thing — the container is ready to serve. Requiring
users to add an additional label duplicates information that Docker already
exposes.

## Functional Requirements

1. After `EnsureRunning` succeeds, if the container has a Docker `HEALTHCHECK`
   configured **and** the `lazy-tcp-proxy.http-healthcheck` label is **not**
   set, the proxy waits for the container's health status to become `healthy`
   before forwarding TCP traffic.

2. Priority order in `handleConn` (first matching rule wins):
   - `lazy-tcp-proxy.http-healthcheck` label set → poll URL (REQ-063 behaviour, unchanged)
   - Docker `HEALTHCHECK` present, no label → wait for `healthy` status via Docker API
   - Neither → existing TCP dial-retry loop (unchanged)

3. The wait polls the Docker API (via `ContainerInspect`) every `dialInterval`
   (1 s) up to the `START_TIMEOUT_SECS` budget. If `unhealthy` or timeout is
   reached, the client connection is dropped with a log message.

4. Containers with health status `none` (no `HEALTHCHECK` configured) are
   treated identically to today — no change in behaviour.

5. **Kubernetes**: the `WaitUntilHealthy` method on the backend interface is a
   no-op that returns `nil` immediately. Kubernetes readiness probes are managed
   by the cluster, not the proxy. The `HasHealthCheck` field in `TargetInfo` is
   always `false` for Kubernetes deployments. This keeps full backward
   compatibility.

6. **UDP**: unchanged. The UDP datagram retry loop already serves as a
   protocol-native readiness probe.

## User Experience Requirements

- Zero configuration — works automatically for any container with a
  `HEALTHCHECK` defined in its image or Compose file.
- No new labels required.
- Log messages indicate progress:
  - `proxy: docker-healthcheck: attempt N: myapp → starting`
  - `proxy: docker-healthcheck: myapp healthy`
  - `proxy: docker-healthcheck: myapp unhealthy after 30s; dropping connection`

## Technical Requirements

- `TargetInfo` gains `HasHealthCheck bool` (set at discovery time).
- `containerBackend` interface gains `WaitUntilHealthy(ctx, containerID, name string, timeout time.Duration) error`.
- Docker `Manager` implements `WaitUntilHealthy`: polls `ContainerInspect →
  Container.State.Health.Status` every `dialInterval` until `"healthy"`,
  `"unhealthy"`, or timeout.
- Kubernetes `Backend` implements `WaitUntilHealthy` as an immediate `nil`
  return (no-op).
- `HasHealthCheck` is populated by inspecting
  `Container.State.Health` at discovery/watch time:
  - `Health != nil && Health.Status != "none"` → `true`
  - Anything else (field absent, `"none"`) → `false`
- `ProxyServer.handleConn` calls `WaitUntilHealthy` only when
  `ts.hasHealthCheck && ts.httpHealthCheck == ""`.

## Acceptance Criteria

- [x] A container with a `HEALTHCHECK` and no `http-healthcheck` label: proxy
      waits for `healthy` before forwarding traffic.
- [x] A container with a `HEALTHCHECK` **and** an `http-healthcheck` label: the
      URL is polled (REQ-063); `WaitUntilHealthy` is NOT called.
- [x] A container with no `HEALTHCHECK` and no label: behaviour identical to
      today.
- [x] `WaitUntilHealthy` times out after `START_TIMEOUT_SECS` and the
      connection is dropped with a log.
- [x] Kubernetes backend: `HasHealthCheck` is always `false`; no API calls made.
- [x] Existing unit and integration tests continue to pass.

## Dependencies

- Extends REQ-063 (HTTP health check label).
- Requires `WaitUntilHealthy` on the `containerBackend` interface — both Docker
  and Kubernetes backends must implement it.

## Implementation Notes

- `ContainerInspect` is already called at discovery time in
  `containerToTargetInfo`. Re-reading `Health.Status` there to set
  `HasHealthCheck` adds no extra API call.
- `WaitUntilHealthy` calls `ContainerInspect` on each poll (once per second);
  this is inexpensive and only happens during cold-start, not during steady-state
  proxying.
- The `unhealthy` status is treated as a terminal failure (no point retrying —
  the container itself considers its service broken).
- `starting` is treated as "not yet ready" and retried.
