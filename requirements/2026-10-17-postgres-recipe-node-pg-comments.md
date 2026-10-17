# Postgres Recipe: Node.js (node-postgres) Connection Comments

**Date Added**: 2026-10-17
**Priority**: Low
**Status**: Completed

## Problem Statement

Connecting to Postgres through Traefik TLS termination from Node.js with `node-postgres` failed with `UNABLE_TO_GET_ISSUER_CERT_LOCALLY`. The actual cause was a missing `sslnegotiation=direct` in the connection string; `pg` defaults to the traditional `SSLRequest`, which Traefik cannot terminate. Direct TLS support only exists from `pg` 8.22.0 (verified by inspecting releases 8.16.0 to 8.23.1). Node also ignores the macOS Keychain and `~/.postgresql/root.crt`, so a trusted root must be supplied explicitly.

## Functional Requirements

1. Extend the top-of-file comment block in `recipes/docker-compose.postgres.5432.yml` (added by REQ-118) with a Node.js section: minimum `pg` version 8.22.0, a `DATABASE_URL` example with `sslmode=verify-full&sslnegotiation=direct`, and the two ways to trust the root CA (`sslrootcert` URL parameter or `NODE_EXTRA_CA_CERTS`).
2. Comments only: no change to services, labels, env vars or behaviour.

## Technical Requirements

- No `${...}` sequences in the comments (Portainer templates endpoint scans recipes for env vars).
- `docker compose config` output must be unchanged.

## Acceptance Criteria

- [x] Comment block documents `pg` ≥ 8.22.0, the `sslnegotiation=direct` URL parameter and root CA trust options.
- [x] `docker compose config` output is unchanged.
- [x] No `${` introduced.

## Dependencies

- Extends REQ-118; relies on REQ-116 labels.

## Implementation Notes

- Verified working by the user: adding `&sslnegotiation=direct` to the `DATABASE_URL` fixed the connection.
- Requested directly by the user; design and plan collapsed into this single lightweight document.
