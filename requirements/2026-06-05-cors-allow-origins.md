# CORS Allow Origins via Environment Variable

**Date Added**: 2026-06-05
**Priority**: Medium
**Status**: Planned

## Problem Statement

When the status dashboard or metrics endpoint is fetched from a different origin (e.g. a local HTML file opened as `file://`, or a separate UI host), the browser blocks the request with a CORS error:

```
Access to fetch at 'http://localhost:8080/metrics' from origin 'null' has been
blocked by CORS policy: No 'Access-Control-Allow-Origin' header is present on
the requested resource.
```

There is currently no way to enable CORS headers without modifying source code.

## Functional Requirements

- Introduce a `CORS_ALLOW_ORIGINS` environment variable.
- When set, the web server adds the following response headers to **every** response:
  - `Access-Control-Allow-Origin: <value of CORS_ALLOW_ORIGINS>`
  - `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
  - `Access-Control-Allow-Headers: Content-Type, X-API-Key, Authorization`
- Pre-flight `OPTIONS` requests must be answered immediately with `204 No Content` and the above headers (so browsers complete the pre-flight handshake).
- When `CORS_ALLOW_ORIGINS` is **not** set, no CORS headers are added (existing behaviour preserved).
- The variable accepts any string value, e.g. `*`, `https://example.com`, or a comma-separated list is left as-is (passed verbatim to the header).

## User Experience Requirements

- Users set `CORS_ALLOW_ORIGINS=*` in their Docker Compose environment section to allow all origins.
- No restart other than the proxy container is required.

## Technical Requirements

- The CORS middleware wraps the entire `http.ServeMux` in `runStatusServer`.
- The same middleware is **not** applied to the admin server (separate mux on a different port) unless explicitly requested in future.
- No external CORS library — implement with a simple `http.Handler` wrapper.

## Acceptance Criteria

- [ ] Setting `CORS_ALLOW_ORIGINS=*` causes `Access-Control-Allow-Origin: *` to appear on `/metrics` responses.
- [ ] Setting `CORS_ALLOW_ORIGINS=https://example.com` causes `Access-Control-Allow-Origin: https://example.com`.
- [ ] `OPTIONS` pre-flight requests return `204` with appropriate headers.
- [ ] Without `CORS_ALLOW_ORIGINS`, no CORS headers are added.
- [ ] `golangci-lint run` passes with no new violations.
- [ ] `go test ./...` passes.

## Dependencies

None. Standalone change to `main.go`.

## Implementation Notes

A small `corsMiddleware(origins string, next http.Handler) http.Handler` function in `main.go` wraps the mux before it is assigned to `hs.Handler`. When `origins == ""` the wrapper is skipped and the mux is used directly.
