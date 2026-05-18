# Traefik Default Environment Variable Values

**Date Added**: 2026-05-18
**Priority**: Low
**Status**: Planned

## Problem Statement

`TRAEFIK_ENTRYPOINT` and `TRAEFIK_CERTRESOLVER` currently default to empty string,
meaning `entryPoints` and `tls` are omitted from generated Traefik router config
unless the user explicitly sets both. Most production Traefik setups use `websecure`
as the HTTPS entry point and a named cert resolver, so the fields should be populated
by default so the proxy works out of the box.

## Functional Requirements

1. `TRAEFIK_ENTRYPOINT` defaults to `"websecure"` when the env var is not set.
2. `TRAEFIK_CERTRESOLVER` defaults to `"myresolver"` when the env var is not set.
3. Users can override either value or set to an empty string to suppress the field
   (by setting `TRAEFIK_ENTRYPOINT=""` explicitly — same escape hatch as today).

## Technical Requirements

- Change `resolveTraefikEntryPoint()` in `main.go` to return `"websecure"` when
  `TRAEFIK_ENTRYPOINT` is unset.
- Change `resolveTraefikCertResolver()` in `main.go` to return `"myresolver"` when
  `TRAEFIK_CERTRESOLVER` is unset.
- Update `README_LABELS.md` and `README_CONFIG.md` default column for both rows.

## Acceptance Criteria

- [ ] `/traefik` output includes `"entryPoints": ["websecure"]` when `TRAEFIK_ENTRYPOINT` is not set.
- [ ] `/traefik` output includes `"tls": {"certResolver": "myresolver"}` when `TRAEFIK_CERTRESOLVER` is not set.
- [ ] Setting `TRAEFIK_ENTRYPOINT=other` overrides to `other`.
- [ ] Documentation defaults updated.

## Dependencies

- REQ-075 Traefik Entrypoint and CertResolver Configuration
