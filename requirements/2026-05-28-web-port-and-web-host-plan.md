# WEB_PORT and WEB_HOST — Implementation Plan

**Requirement**: [2026-05-28-web-port-and-web-host.md](2026-05-28-web-port-and-web-host.md)
**Date**: 2026-05-28
**Status**: Implemented

## Implementation Steps

1. **`main.go` — replace `resolveStatusPort` with `resolveWebPort`**
   - Rename `resolveStatusPort()` → `resolveWebPort()`.
   - Resolution order: `WEB_PORT` → `STATUS_PORT` (fallback, backward compat) → `8080`.
   - Update the single call site in `main()`.

2. **`main.go` — add `resolveWebHost`**
   - New function that returns `os.Getenv("WEB_HOST")` — empty string if unset, no default.

3. **`main.go` — thread `webHost` into the status server and traefik handler**
   - `runStatusServer` receives two new parameters: `webHost string` and `webPort int`.
   - Inside the `/traefik` handler, pass `webHost` and `webPort` to `BuildConfig`.
   - Update the startup log line to include `WEB_HOST` status.

4. **`internal/traefik/config.go` — extend `BuildConfig` signature**
   - New signature: `BuildConfig(snapshots []Snapshot, proxyHost, entryPoint, certResolver, webHost string, webPort int) TraefikConfig`
   - When `webHost != ""`:
     - Compute name: `sanitiseName(fmt.Sprintf("%s-%d", webHost, webPort))`
     - Add HTTP router: rule `Host('<webHost>')`, service `<name>-service`, apply `entryPoint`/`certResolver` same as all other routers.
     - Add HTTP service: URL `http://<proxyHost>:<webPort>`.
   - When `webHost == ""`: no change to output (existing behaviour preserved).

5. **`internal/traefik/config_test.go` — update all existing calls + add new tests**
   - All existing `BuildConfig(...)` calls gain two trailing args `"", 0` (no behavioural change).
   - New test `TestBuildConfig_WebHost`: `webHost="admin.foobar.com"`, `webPort=8080`,
     `proxyHost="lazy-tcp-proxy"` → router rule `Host('admin.foobar.com')`, service URL
     `http://lazy-tcp-proxy:8080`.
   - New test `TestBuildConfig_WebHostEmpty`: `webHost=""` → no extra router/service beyond
     what snapshots produce.
   - New test `TestBuildConfig_WebHostWithEntryPointAndCertResolver`: confirm `entryPoint` and
     `certResolver` apply to the web host route.

6. **`README.md` — update env var table**
   - Add `WEB_PORT` row (defaults to `8080`, note `STATUS_PORT` is a legacy alias).
   - Add `WEB_HOST` row (no default; when set, exposes the web dashboard via Traefik).
   - Keep `STATUS_PORT` row with a note that `WEB_PORT` takes precedence.

7. **`README_CONFIG.md` — update env var table**
   - Same additions as step 6 (the Traefik section in this file already documents related vars).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/main.go` | Modify | `resolveStatusPort` → `resolveWebPort` (WEB_PORT first, STATUS_PORT fallback); add `resolveWebHost`; thread params through |
| `lazy-tcp-proxy/internal/traefik/config.go` | Modify | `BuildConfig` gains `webHost string, webPort int`; adds route when `webHost != ""` |
| `lazy-tcp-proxy/internal/traefik/config_test.go` | Modify | Update all call sites; add 3 new tests |
| `README.md` | Modify | Add `WEB_PORT`, `WEB_HOST` rows; clarify `STATUS_PORT` |
| `README_CONFIG.md` | Modify | Add `WEB_PORT`, `WEB_HOST` rows |

## Key Code Snippets

### `config.go` — web host route (inside `BuildConfig`)

```go
if webHost != "" {
    name := sanitiseName(fmt.Sprintf("%s-%d", webHost, webPort))
    router := HTTPRouter{
        Rule:    fmt.Sprintf("Host(`%s`)", webHost),
        Service: name + "-service",
    }
    if entryPoint != "" {
        router.EntryPoints = []string{entryPoint}
    }
    if certResolver != "" {
        router.TLS = &RouterTLS{CertResolver: certResolver}
    }
    httpRouters[name+"-router"] = router
    httpServices[name+"-service"] = HTTPService{
        LoadBalancer: LoadBalancer{
            Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", proxyHost, webPort)}},
        },
    }
}
```

### `main.go` — `resolveWebPort`

```go
func resolveWebPort() int {
    if raw := os.Getenv("WEB_PORT"); raw != "" {
        if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
            return n
        }
        log.Printf("WEB_PORT=%q is invalid; checking STATUS_PORT", raw)
    }
    if raw := os.Getenv("STATUS_PORT"); raw != "" {
        if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
            return n
        }
        log.Printf("STATUS_PORT=%q is invalid; using default %d", raw, defaultWebPort)
    }
    return defaultWebPort
}
```

## Unit Tests

| Test | Input | Expected |
|------|-------|----------|
| `TestBuildConfig_WebHost` | `webHost="admin.foobar.com"`, `webPort=8080`, `proxyHost="lazy-tcp-proxy"` | router `Host('admin.foobar.com')` + service URL `http://lazy-tcp-proxy:8080` |
| `TestBuildConfig_WebHostEmpty` | `webHost=""`, `webPort=0` | no extra routers/services |
| `TestBuildConfig_WebHostWithEntryPointAndCertResolver` | `webHost="admin.foobar.com"`, `entryPoint="websecure"`, `certResolver="myresolver"` | router has `entryPoints=["websecure"]` and `tls.certResolver="myresolver"` |
| All existing tests | `"", 0` appended | unchanged output (backward compat) |

## Risks & Open Questions

- `STATUS_PORT` fallback: the existing `resolveStatusPort` logs under the name `STATUS_PORT`;
  the new function should log under `WEB_PORT` / `STATUS_PORT` as appropriate.
- The `defaultWebPort` constant replaces `defaultStatusPort` — both are `8080`.
