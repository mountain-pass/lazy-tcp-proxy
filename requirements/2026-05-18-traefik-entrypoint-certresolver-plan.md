# Traefik Entrypoint and CertResolver Configuration — Implementation Plan

**Requirement**: [2026-05-18-traefik-entrypoint-certresolver.md](2026-05-18-traefik-entrypoint-certresolver.md)
**Date**: 2026-05-18
**Status**: Approved

## Implementation Steps

1. **`internal/traefik/config.go`** — extend data model and `BuildConfig` signature
   - Add `RouterTLS` struct with `CertResolver string \`json:"certResolver,omitempty"\``
   - Add `EntryPoints []string \`json:"entryPoints,omitempty"\`` to `HTTPRouter`
   - Add `TLS *RouterTLS \`json:"tls,omitempty"\`` to `HTTPRouter`
   - Change `BuildConfig` signature to `BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver string)`
   - When `entryPoint != ""`, set `EntryPoints: []string{entryPoint}` on each emitted router
   - When `certResolver != ""`, set `TLS: &RouterTLS{CertResolver: certResolver}` on each emitted router

2. **`internal/traefik/config_test.go`** — add 4 new test cases
   - `TestBuildConfig_WithEntryPoint` — sets entryPoint, asserts `entryPoints: ["websecure"]`
   - `TestBuildConfig_WithCertResolver` — sets certResolver, asserts `tls.certResolver`
   - `TestBuildConfig_WithBoth` — both set; asserts both fields present
   - `TestBuildConfig_NeitherSet` — both empty; asserts fields absent (existing behaviour preserved)
   - Update all existing tests to pass `""` for the two new parameters

3. **`main.go`** — resolver functions, wiring, and default change
   - Change `const defaultAdminPort = 8081` → `const defaultAdminPort = 0`
   - Add `resolveTraefikEntryPoint() string` — returns `os.Getenv("TRAEFIK_ENTRYPOINT")`
   - Add `resolveTraefikCertResolver() string` — returns `os.Getenv("TRAEFIK_CERTRESOLVER")`
   - Update `runStatusServer` signature: `(ctx, srv, port, traefikProxyHost, entryPoint, certResolver string)`
   - In the `/traefik` handler, pass `entryPoint` and `certResolver` to `BuildConfig`
   - Resolve and pass from `main()`: `runStatusServer(ctx, srv, statusPort, traefikProxyHost, resolveTraefikEntryPoint(), resolveTraefikCertResolver())`
   - Update log line to include new vars when set

4. **`README_LABELS.md`** — add two rows to the environment variables table
   - `TRAEFIK_ENTRYPOINT` | *(unset)* | Traefik entry point name added to every generated router's `entryPoints`
   - `TRAEFIK_CERTRESOLVER` | *(unset)* | Cert resolver name added to every generated router's `tls.certResolver`

5. **`README_CONFIG.md`** — add same two rows to the environment variables table

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/traefik/config.go` | Modify | Add `RouterTLS` type, extend `HTTPRouter`, update `BuildConfig` signature |
| `lazy-tcp-proxy/internal/traefik/config_test.go` | Modify | Update existing tests + add 4 new test cases |
| `lazy-tcp-proxy/main.go` | Modify | Change default admin port, add two resolver functions, thread params through |
| `README_LABELS.md` | Modify | Add two env-var rows to Traefik integration table |
| `README_CONFIG.md` | Modify | Add two env-var rows to environment variables table |

## API Contracts

`GET /traefik` with `TRAEFIK_ENTRYPOINT=websecure` and `TRAEFIK_CERTRESOLVER=myresolver`:

```json
{
  "http": {
    "routers": {
      "whoami-localhost-9001-router": {
        "rule": "Host(`whoami.localhost`)",
        "service": "whoami-localhost-9001-service",
        "entryPoints": ["websecure"],
        "tls": { "certResolver": "myresolver" }
      }
    },
    "services": { ... }
  }
}
```

## Key Code Snippets

```go
// New type
type RouterTLS struct {
    CertResolver string `json:"certResolver,omitempty"`
}

// Updated HTTPRouter
type HTTPRouter struct {
    Rule        string    `json:"rule"`
    Service     string    `json:"service"`
    EntryPoints []string  `json:"entryPoints,omitempty"`
    TLS         *RouterTLS `json:"tls,omitempty"`
}

// Updated BuildConfig signature
func BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver string) TraefikConfig {
    // ...
    router := HTTPRouter{Rule: ..., Service: ...}
    if entryPoint != "" {
        router.EntryPoints = []string{entryPoint}
    }
    if certResolver != "" {
        router.TLS = &RouterTLS{CertResolver: certResolver}
    }
    routers[routerName] = router
}
```

## Unit Tests

| Test | entryPoint | certResolver | Expected router fields |
|------|-----------|--------------|------------------------|
| WithEntryPoint | `"websecure"` | `""` | `entryPoints: ["websecure"]`, no `tls` |
| WithCertResolver | `""` | `"myresolver"` | no `entryPoints`, `tls.certResolver: "myresolver"` |
| WithBoth | `"websecure"` | `"myresolver"` | both fields present |
| NeitherSet | `""` | `""` | neither field present |
| (all existing) | `""` | `""` | unchanged behaviour |

## Risks & Open Questions

- None — straightforward additive change. All new fields use `omitempty` so no breaking change to existing deployments.
