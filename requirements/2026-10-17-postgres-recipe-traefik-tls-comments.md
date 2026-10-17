# Postgres Recipe: Traefik TLS Connection Comments

**Date Added**: 2026-10-17
**Priority**: Low
**Status**: Completed

## Problem Statement

Connecting to Postgres through Traefik TLS termination needs three non-obvious things: the `alpn-protocols=postgresql` label, a libpq 17+ client using `sslnegotiation=direct`, and (for pgAdmin / some libpq builds where `sslrootcert=system` finds no CA store) an explicit root CA file. None of this was in the recipe people copy from.

## Functional Requirements

1. Add a comment block at the top of `recipes/docker-compose.postgres.5432.yml` containing: the Traefik labels (`traefik-tcp-hosts`, `alpn-protocols`, optional `min-version`), the `psql` connection command, and the `curl` command that installs the ISRG Root X1 certificate as `~/.postgresql/root.crt`.
2. Comments only: no change to services, labels, env vars or behaviour.

## Technical Requirements

- The comments must not contain `${...}` sequences (the Portainer templates endpoint scans recipes for env vars).
- The file must still validate with `docker compose config`.

## Acceptance Criteria

- [x] Top-of-file comments include labels, `psql` command and `curl` root CA command.
- [x] `docker compose -f recipes/docker-compose.postgres.5432.yml config` output is unchanged.
- [x] No `${` introduced in comments.

## Dependencies

- Builds on REQ-116 (alpn-protocols / min-version labels).

## Implementation Notes

- Requested directly by the user; design and plan collapsed into this single lightweight document.
- A fuller "Connecting and troubleshooting" table in `README_LABELS.md` was proposed but not requested; not done here.
