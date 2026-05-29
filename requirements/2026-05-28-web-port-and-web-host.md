# WEB_PORT and WEB_HOST Environment Variables

**Date Added**: 2026-05-28
**Priority**: Medium
**Status**: In Progress

## Problem Statement

lazy-tcp-proxy's HTTP server (serving `/status`, `/traefik`, `/health`, and the dashboard)
is currently configured via `STATUS_PORT`, which doesn't convey that the same server also
provides the Traefik HTTP provider endpoint and the status dashboard.

There is also no way to expose lazy-tcp-proxy's own web endpoint via a Traefik hostname.
The `/traefik` endpoint generates routes for managed Docker services, but not for
lazy-tcp-proxy's own dashboard — users who want `http://proxy.localhost` to open the
lazy-tcp-proxy status page have no supported mechanism to configure this.

## Functional Requirements

### 1. `WEB_PORT` — configure the web server listen port

Replace `STATUS_PORT` with `WEB_PORT`. Same semantics: integer, defaults to `8080`, `0`
disables the server.

**Backward compatibility**: `STATUS_PORT` continues to work as a fallback. Resolution order:
1. `WEB_PORT` (if set and valid)
2. `STATUS_PORT` (if set and valid, for backward compat)
3. default `8080`

### 2. `WEB_HOST` — expose lazy-tcp-proxy's web endpoint via Traefik

`WEB_HOST` is an optional hostname. When set, the `/traefik` endpoint includes one
additional HTTP router+service pair that routes `Host('<WEB_HOST>')` to lazy-tcp-proxy's
own web server:

- **Router rule**: `Host('<WEB_HOST>')`
- **Service URL**: `http://<TRAEFIK_PROXY_HOST>:<WEB_PORT>`
  (`TRAEFIK_PROXY_HOST` is the existing env var; `WEB_PORT` is the resolved listen port)
- Router name: `sanitiseName("<WEB_HOST>-<WEB_PORT>") + "-router"` (same convention as all other routes)
- Service name: `sanitiseName("<WEB_HOST>-<WEB_PORT>") + "-service"`
- `entryPoints` and `tls.certResolver` follow `TRAEFIK_ENTRYPOINT` / `TRAEFIK_CERTRESOLVER`
  exactly as all other HTTP routers do.

When `WEB_HOST` is **not** set, no route is generated for the web port — the `/traefik`
response is unchanged from the current behaviour.

### 3. Startup log

When the web server starts, log the value of `WEB_PORT` (and note that `WEB_HOST` is
set or unset):

```
web server: listening on :8080 (WEB_HOST=proxy.localhost)
```

## User Experience Requirements

Minimal docker-compose example with both variables:

```yaml
environment:
  WEB_PORT: 8080
  WEB_HOST: proxy.localhost
  TRAEFIK_PROXY_HOST: lazy-tcp-proxy
```

With these settings, `curl -H "Host: proxy.localhost" http://localhost` (via Traefik on :80)
returns lazy-tcp-proxy's status dashboard HTML.

## Technical Requirements

- `resolveStatusPort()` in `main.go` is replaced by `resolveWebPort()`:
  reads `WEB_PORT` first, then `STATUS_PORT`, then defaults to `8080`.
- `resolveWebHost()` reads `WEB_HOST`; returns `""` if unset.
- `runStatusServer()` receives `webHost string` in place of / alongside existing parameters.
- `BuildConfig()` in `internal/traefik/config.go` receives a new `WebHost` field (or
  separate string parameters `webHost string, webPort int`). When `webHost != ""`, a
  route is prepended/appended to the HTTP section.
- All existing env-var references to `STATUS_PORT` in READMEs and documentation updated
  to `WEB_PORT` (noting backward compat).
- The placeholder comment in the generated config YAML file is unaffected.

## Acceptance Criteria

- [ ] `WEB_PORT=9090` causes the web server to listen on `:9090`.
- [ ] `STATUS_PORT=9090` (no `WEB_PORT` set) still causes the web server to listen on `:9090`
      (backward compat).
- [ ] `WEB_PORT=0` disables the web server.
- [ ] `WEB_HOST=proxy.localhost` with `TRAEFIK_PROXY_HOST=lazy-tcp-proxy` and `WEB_PORT=8080`
      causes `GET /traefik` to include a router with rule `Host('proxy.localhost')` and service
      URL `http://lazy-tcp-proxy:8080`.
- [ ] `WEB_HOST` unset: `GET /traefik` contains no route for the web port (current behaviour preserved).
- [ ] `TRAEFIK_ENTRYPOINT` and `TRAEFIK_CERTRESOLVER` apply to the `WEB_HOST` route just as
      they do to other HTTP routes.
- [ ] Startup log includes resolved port and `WEB_HOST` value (or "not set").
- [ ] Documentation (README_LABELS.md or env-var tables) updated.

## Dependencies

- REQ-025 (HTTP Status Endpoint) — web server this configures.
- REQ-069 (Traefik Integration) — `/traefik` endpoint the `WEB_HOST` route is added to.
- REQ-075 (Traefik Entrypoint and CertResolver) — `TRAEFIK_ENTRYPOINT`/`TRAEFIK_CERTRESOLVER`
  apply to the new route.

## Implementation Notes

- `sanitiseName` already handles domain strings; no changes needed there.
- The `WEB_HOST` route is conceptually identical to a service `traefik_hosts` entry — it
  just comes from an env var rather than a managed container's metadata.
- No changes to `types.TargetInfo` or `config.ServiceEntry` are required.
