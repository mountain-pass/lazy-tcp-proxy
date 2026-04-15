# HTTP Health Check Label for Container Readiness

**Date Added**: 2026-04-14
**Priority**: Medium
**Status**: Completed

## Problem Statement

Some Docker containers accept TCP connections on their service port before the
application layer is fully initialised (e.g. a database that binds its port
during startup but finishes migration/bootstrap seconds later). The existing
TCP dial-retry loop detects when the port is *open*, but has no way to know
whether the service is *ready* to handle requests.

A common pattern in container orchestration is to expose a lightweight HTTP
readiness endpoint (e.g. `/health`, `/status`) that returns 503 while
initialising and 200 once ready. Polling this endpoint is the most reliable
signal available for these services.

## Functional Requirements

1. A new optional container label (Docker) / annotation (Kubernetes):
   `lazy-tcp-proxy.http-healthcheck=<url>`

2. The URL supports a `${container}` placeholder that is substituted with
   the container's actual name at parse time, so users don't need to hardcode
   internal hostnames:
   ```
   lazy-tcp-proxy.http-healthcheck=http://${container}:8080/health
   ```

3. **When the label is present (TCP connections)**:
   - After `EnsureRunning` succeeds, the proxy polls the URL with HTTP GET
     before establishing the upstream TCP connection.
   - Polling repeats every `dialInterval` (1 second) until either:
     - A **2xx** HTTP status code is received → proceed to proxy; or
     - The `START_TIMEOUT_SECS` budget is exhausted → drop the client
       connection (same behaviour as an unreachable upstream today).
   - Non-2xx responses (e.g. 503) and connection errors are both treated as
     "not yet ready" and retried.

4. **When the label is absent**: no change to existing behaviour — TCP uses the
   existing dial-retry loop; UDP uses the existing datagram readiness probe.

5. UDP behaviour is **unchanged**. The UDP datagram retry loop already acts as
   a protocol-native readiness probe and does not need an HTTP alternative.

6. The polling HTTP client uses the same `webhookClient` (`*http.Client` with
   5 s timeout per request) already present on `ProxyServer`, so no new
   dependency is added.

## User Experience Requirements

- Label is optional per container; mixing labelled and unlabelled containers in
  the same proxy instance is fully supported.
- Placeholder is case-sensitive: `${container}` only (lowercase).
- An invalid URL (unparseable) is logged as a warning at discovery time and the
  label is ignored (same pattern as `webhook-url`).
- Log messages indicate poll attempts and outcome, e.g.:
  - `proxy: http-health-check: attempt 1: GET http://myapp:8080/health → 503`
  - `proxy: http-health-check: upstream myapp ready (200)`
  - `proxy: http-health-check: upstream myapp not ready after 30s; dropping connection`

## Technical Requirements

- Substitution of `${container}` happens inside `containerToTargetInfo`
  (Docker) and `deploymentToTargetInfo` (Kubernetes) immediately after the
  container/deployment name is known.
- `TargetInfo` gains a new field: `HTTPHealthCheck string` (empty = disabled).
- `targetState` in `server.go` gains a matching `httpHealthCheck string` field.
- A new helper `waitForHTTPReady(ctx, client, url, timeout, interval, name) error`
  in `server.go` encapsulates the polling loop.
- The helper is called in `handleConn` after `EnsureRunning` and before the
  existing dial-retry loop, only when `ts.httpHealthCheck != ""`.

## Acceptance Criteria

- [ ] Label `lazy-tcp-proxy.http-healthcheck=http://${container}:8080/health`
      is parsed correctly for Docker containers; `${container}` is replaced
      with the actual container name.
- [ ] Same annotation is parsed correctly for Kubernetes Deployments.
- [ ] When the label is set and the endpoint returns 503 initially then 200,
      the proxy waits and connects only after the 200 response.
- [ ] When the label is set and the endpoint never returns 2xx within
      `START_TIMEOUT_SECS`, the client connection is dropped with a log.
- [ ] When the label is absent, TCP proxy behaviour is identical to today.
- [ ] An invalid URL in the label is logged and ignored (label treated as absent).
- [ ] Concurrent cold-start connections share the same `singleflight` container
      start, but each polls the health URL independently (the health check is
      per-connection, not shared — it is cheap and fast once the service is up).
- [ ] Existing unit and integration tests continue to pass.

## Dependencies

- Builds on REQ-062 (TCP dial-retry loop wired to `START_TIMEOUT_SECS`).
- Reuses `webhookClient` already on `ProxyServer`.
- Does not affect UDP behaviour (REQ-027, REQ-055, REQ-057, REQ-061).

## Implementation Notes

### Polling interval and timeout

The poll interval reuses `dialInterval` (1 second, the same constant used by
the TCP dial-retry loop). The total polling budget is `START_TIMEOUT_SECS`
(same as the TCP dial-retry timeout), so the number of HTTP attempts is
`ceil(startTimeout / dialInterval)`.

### URL placeholder

Substitution is a simple `strings.ReplaceAll(raw, "${container}", name)`
before `url.ParseRequestURI` validation.

### Singleflight and health check

`EnsureRunning` is already deduplicated via `singleflight`. The HTTP health
check runs *after* the singleflight returns, so each goroutine polls
independently. This is intentional: the poll is a lightweight GET (typically
<1 ms RTT once the service is up) and avoids a second shared-state coordinator.

### Label name

`lazy-tcp-proxy.http-healthcheck` (single hyphen between `http` and
`healthcheck`) to make the intent (readiness gate) unambiguous and avoid
confusion with the proxy's own `/status` endpoint.
