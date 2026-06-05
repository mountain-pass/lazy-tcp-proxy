# CORS Allow Origins — Implementation Plan

**Requirement**: [2026-06-05-cors-allow-origins.md](2026-06-05-cors-allow-origins.md)
**Date**: 2026-06-05
**Status**: Implemented

## Implementation Steps

1. Add `resolveCORSAllowOrigins()` helper in `main.go` — reads `CORS_ALLOW_ORIGINS` env var, returns empty string if unset.
2. Add `corsMiddleware(origins string, next http.Handler) http.Handler` in `main.go` — when `origins != ""`, wraps handler to set CORS headers and handle OPTIONS pre-flight.
3. In `runStatusServer`, wrap the mux with `corsMiddleware` before assigning to `hs.Handler`.
4. Thread `corsOrigins string` parameter through `runStatusServer` signature and the call site in `main()`.
5. Update requirement status to Completed.
6. Run `golangci-lint run` and `go test ./...`, fix any issues.
7. Commit and push all changes, open PR.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/main.go` | Modify | Add `resolveCORSAllowOrigins`, `corsMiddleware`, update `runStatusServer` signature and body |

## Key Code Snippets

```go
func resolveCORSAllowOrigins() string {
    return os.Getenv("CORS_ALLOW_ORIGINS")
}

func corsMiddleware(origins string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", origins)
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusNoContent)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| CORS headers present | `CORS_ALLOW_ORIGINS=*`, GET /metrics | `Access-Control-Allow-Origin: *` in response |
| OPTIONS pre-flight | `CORS_ALLOW_ORIGINS=*`, OPTIONS /metrics | 204, CORS headers |
| No CORS when unset | `CORS_ALLOW_ORIGINS` unset | No `Access-Control-Allow-Origin` header |

## Risks & Open Questions

None. Self-contained, low-risk change.
