# Fix: Omit Empty `http` Section from `/traefik` Payload

**Date Added**: 2026-10-17
**Priority**: High
**Status**: Completed

## Problem Statement

When there are no HTTP routers (a TCP-only setup: only `traefik-tcp-hosts`, no `traefik-hosts` and no `WEB_HOST`), `GET /traefik` still emitted `"http": {"routers": {}, "services": {}, ...}`. Traefik v3.1's HTTP provider rejects the entire payload with `cannot decode configuration data: routers cannot be a standalone element`, so none of the TCP routers are applied. Reproduced against a real Traefik v3.1 container, both with and without the default `serversTransports` section.

Found while validating REQ-116; it is independent of the TLS options work.

## Functional Requirements

1. `/traefik` must not emit empty `http.routers` / `http.services` maps.
2. The whole `http` key must be omitted when the HTTP section has no routers, no services and no serversTransports.
3. When there are HTTP routers (from `traefik-hosts` or `WEB_HOST`), the output is unchanged.
4. With no targets at all, the payload is `{}`.

## Technical Requirements

- `HTTPConfig.Routers` / `Services` get `omitempty`; `HTTPConfig` implements `IsZero()` and `TraefikConfig.HTTP` uses `omitzero` (Go ≥ 1.24; module is Go 1.25).
- `serversTransports` is left as is: a TCP-only payload with only the default `lazy-transport` under `http` was verified to be accepted by Traefik v3.1.

## Acceptance Criteria

- [x] A TCP-only config marshals without an `http` key.
- [x] A config with no targets marshals to `{}`.
- [x] Configs with HTTP hosts or `WEB_HOST` still contain the `http` section with routers and services.
- [x] Real Traefik v3.1 accepts a TCP-only payload (including default `serversTransports`) and adds the TCP routers; the old empty-maps shape reproduces the error.
- [x] Unit tests added; `golangci-lint run` and `go test ./...` pass.

## Dependencies

- Found during REQ-116 (per-service TLS options); touches the same `internal/traefik/config.go`.

## Implementation Notes

- This was requested directly by the user right after being reported as a pre-existing issue; the design and plan phases were collapsed into this single lightweight document and implementation proceeded on that explicit instruction.
- Files: `internal/traefik/config.go` (tags + `IsZero`), `internal/traefik/config_test.go` (4 tests: TCP-only omits `http`; no targets → `{}`; HTTP host keeps section; `WEB_HOST`-only keeps section).
- Live check (Traefik v3.1, HTTP provider): TCP-only payload with default `serversTransports` → TLS route added, no decode error; old payload with empty `routers`/`services` → `routers cannot be a standalone element`.
