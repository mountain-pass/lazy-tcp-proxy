# Traefik Entrypoint and CertResolver Configuration

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Planned

## Problem Statement

The `/traefik` HTTP provider endpoint currently emits routers with no `entryPoints`
or TLS configuration, which means Traefik applies them to all entry points and without
TLS. In production, users need to target a specific entry point (e.g. `websecure`) and
specify a cert resolver (e.g. a Let's Encrypt resolver) so that Traefik automatically
provisions HTTPS certificates for the proxied domains.

Additionally, the `ADMIN_PORT` environment variable defaults to `8081` (enabled), which
exposes the admin API without an intentional opt-in. It should default to `0` (disabled)
so users must explicitly enable it.

## Functional Requirements

1. Add `TRAEFIK_ENTRYPOINT` environment variable.
   - When set, its value is used as the sole entry in the `entryPoints` array on every
     generated Traefik HTTP router.
   - When not set, `entryPoints` is omitted from the router (current behaviour preserved).

2. Add `TRAEFIK_CERTRESOLVER` environment variable.
   - When set, its value is used as `tls.certResolver` on every generated Traefik HTTP router.
   - When not set, the `tls` object is omitted from the router.

3. Change the default value of `ADMIN_PORT` from `8081` to `0` (disabled).

## User Experience Requirements

- Users configure both variables in their Docker Compose environment section, e.g.:
  ```yaml
  environment:
    TRAEFIK_ENTRYPOINT: websecure
    TRAEFIK_CERTRESOLVER: myresolver
  ```
- The generated `/traefik` JSON for a host entry `whoami.localhost:9001` with both
  variables set would include:
  ```json
  "entryPoints": ["websecure"],
  "tls": { "certResolver": "myresolver" }
  ```

## Technical Requirements

- Changes are confined to `internal/traefik/config.go`, `main.go`, and tests.
- `BuildConfig` receives `entryPoint` and `certResolver` strings as additional parameters.
  Empty string = omit the field.
- `HTTPRouter` gains `EntryPoints []string` (`json:"entryPoints,omitempty"`) and
  `TLS *RouterTLS` (`json:"tls,omitempty"`).
- New type `RouterTLS struct { CertResolver string \`json:"certResolver,omitempty"\` }`.
- Documentation updated: `README_LABELS.md` and `README_CONFIG.md` env-var tables.

## Acceptance Criteria

- [ ] `/traefik` output includes `"entryPoints": ["websecure"]` when `TRAEFIK_ENTRYPOINT=websecure`.
- [ ] `/traefik` output includes `"tls": {"certResolver": "myresolver"}` when `TRAEFIK_CERTRESOLVER=myresolver`.
- [ ] `entryPoints` field is absent when `TRAEFIK_ENTRYPOINT` is not set.
- [ ] `tls` field is absent when `TRAEFIK_CERTRESOLVER` is not set.
- [ ] `ADMIN_PORT` defaults to `0` (admin API disabled unless explicitly configured).
- [ ] Unit tests cover all four combinations of the two new env vars.
- [ ] Documentation updated.

## Dependencies

- REQ-069 Traefik Integration (HTTP Provider Endpoint)

## Implementation Notes

`entryPoints` and `certResolver` are independent — a user may set one without the other
(e.g. specify entry point without cert resolver for HTTP-only Traefik setups).
