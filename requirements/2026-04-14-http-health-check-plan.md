# HTTP Health Check Label for Container Readiness — Implementation Plan

**Requirement**: [2026-04-14-http-health-check.md](2026-04-14-http-health-check.md)
**Date**: 2026-04-14
**Status**: Implemented

## Implementation Steps

1. **`internal/types/types.go`** — add `HTTPHealthCheck string` field to `TargetInfo` and a `ParseHTTPHealthCheckLabel` helper that validates the URL and substitutes `${container}`.

2. **`internal/docker/manager.go`** — parse `lazy-tcp-proxy.http-healthcheck` label in `containerToTargetInfo`; call the new helper and assign to `info.HTTPHealthCheck`.

3. **`internal/k8s/backend.go`** — parse the same annotation in `deploymentToTargetInfo`; same call pattern as Docker.

4. **`internal/proxy/server.go`**:
   a. Add `httpHealthCheck string` field to `targetState`.
   b. Populate it in `RegisterTarget` (TCP path) from `info.HTTPHealthCheck`.
   c. Add `waitForHTTPReady(ctx, url, timeout, name string) error` helper.
   d. Call `waitForHTTPReady` inside `handleConn` after `EnsureRunning` succeeds and before the dial-retry loop, gated on `ts.httpHealthCheck != ""`.

5. **`internal/proxy/server_test.go`** (or new test file) — add unit tests for `waitForHTTPReady` using an `httptest.Server`.

6. **`internal/types/types_test.go`** — add tests for `ParseHTTPHealthCheckLabel` covering: valid URL, `${container}` substitution, empty value (nil/no-op), invalid URL (ignored with warning).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | Add `HTTPHealthCheck` to `TargetInfo`; add `ParseHTTPHealthCheckLabel` |
| `internal/types/types_test.go` | Modify | Tests for `ParseHTTPHealthCheckLabel` |
| `internal/docker/manager.go` | Modify | Parse `lazy-tcp-proxy.http-healthcheck` label |
| `internal/k8s/backend.go` | Modify | Parse `lazy-tcp-proxy.http-healthcheck` annotation |
| `internal/proxy/server.go` | Modify | Add `httpHealthCheck` to `targetState`; wire in `RegisterTarget`; add `waitForHTTPReady`; call it in `handleConn` |
| `internal/proxy/server_test.go` | Modify | Unit tests for `waitForHTTPReady` |

## Key Code Snippets

### `types.go` — new helper

```go
// ParseHTTPHealthCheckLabel validates and returns the http-healthcheck URL,
// substituting ${container} with the actual container name.
// Returns "" if the value is absent, empty, or not a valid URL.
func ParseHTTPHealthCheckLabel(containerName, raw string) string {
    v := strings.TrimSpace(raw)
    if v == "" {
        return ""
    }
    v = strings.ReplaceAll(v, "${container}", containerName)
    if _, err := url.ParseRequestURI(v); err != nil {
        log.Printf("container %s: ignoring invalid http-healthcheck URL %q: %v", containerName, v, err)
        return ""
    }
    return v
}
```

### `server.go` — `waitForHTTPReady`

```go
// waitForHTTPReady polls url with HTTP GET every dialInterval until a 2xx
// response is received or the timeout is exceeded. Returns nil on success.
func (s *ProxyServer) waitForHTTPReady(ctx context.Context, url, name string, timeout time.Duration) error {
    retries := int((timeout + dialInterval - 1) / dialInterval)
    if retries < 1 {
        retries = 1
    }
    for attempt := 1; attempt <= retries; attempt++ {
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
            return fmt.Errorf("building request: %w", err)
        }
        resp, err := s.webhookClient.Do(req)
        if err == nil {
            resp.Body.Close()
            if resp.StatusCode >= 200 && resp.StatusCode < 300 {
                log.Printf("proxy: http-healthcheck: \033[33m%s\033[0m ready (%d)", name, resp.StatusCode)
                return nil
            }
            log.Printf("proxy: http-healthcheck: attempt %d: \033[33m%s\033[0m → %d", attempt, name, resp.StatusCode)
        } else {
            log.Printf("proxy: http-healthcheck: attempt %d: \033[33m%s\033[0m → %v", attempt, name, err)
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(dialInterval):
        }
    }
    return fmt.Errorf("upstream \033[33m%s\033[0m not ready after %s", name, timeout)
}
```

### `server.go` — call site in `handleConn`

```go
// After EnsureRunning succeeds, before dial-retry loop:
if ts.httpHealthCheck != "" {
    if err := s.waitForHTTPReady(ctx, ts.httpHealthCheck, ts.info.ContainerName, ts.startTimeout); err != nil {
        log.Printf("proxy: http-healthcheck: %v; dropping connection", err)
        return
    }
}
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `ParseHTTPHealthCheckLabel` — valid URL | `"http://myapp:8080/health"`, name=`"myapp"` | `"http://myapp:8080/health"` |
| `ParseHTTPHealthCheckLabel` — placeholder | `"http://${container}:8080/health"`, name=`"myapp"` | `"http://myapp:8080/health"` |
| `ParseHTTPHealthCheckLabel` — empty | `""` | `""` |
| `ParseHTTPHealthCheckLabel` — invalid URL | `"not-a-url"` | `""` (with log) |
| `waitForHTTPReady` — immediate 200 | server returns 200 on first call | returns nil after 1 attempt |
| `waitForHTTPReady` — 503 then 200 | server returns 503 twice then 200 | returns nil after 3 attempts |
| `waitForHTTPReady` — always 503 | server always 503, timeout=2s | returns error |
| `waitForHTTPReady` — ctx cancelled | ctx cancelled mid-loop | returns ctx.Err() |

## Risks & Open Questions

- `webhookClient` has a 5 s per-request timeout. If the health endpoint itself
  hangs for 5 s, each retry takes 5 s + `dialInterval`, which could exhaust
  `START_TIMEOUT_SECS` faster than expected. This is acceptable; the 5 s per-
  request timeout is a safety rail, not an expected code path.
- The health check runs per-connection (not shared across concurrent cold-start
  connections). This is intentional: once the service is up, each GET completes
  in milliseconds, so the duplication cost is negligible.
