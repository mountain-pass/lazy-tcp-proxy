# Self-Contained Postgres SSL Recipe — Implementation Plan

**Requirement**: [2026-10-16-self-contained-postgres-ssl-recipe.md](2026-10-16-self-contained-postgres-ssl-recipe.md)
**Date**: 2026-10-16
**Status**: Draft

## Decisions Applied

Open questions from the design were not overridden, so the recommendations apply: **Option A** (top-level `configs` with inline `content:`), no migration of old certs, and a comment explaining how to extract `ca.crt`.

## Implementation Steps

1. Rewrite `recipes/docker-compose.postgres-ssl.5432.yml` as a single file:
   - Top-level `configs`:
     - `generate_certs`: the body of `generate-certs.sh` (see snippet notes below).
     - `pg_hba`: the body of `pg_hba.conf`.
   - Service `ssl-init` (alpine:3.20, replaces `ssl-cert-init`):
     - Mounts configs read-only at `/scripts/generate-certs.sh` and `/scripts/pg_hba.conf`.
     - Mounts named volumes `postgres_ssl_certs:/certs` and `postgres_ssl_conf:/conf`.
     - `command`: `apk add --no-cache openssl`, then run the cert script, then `cp /scripts/pg_hba.conf /conf/pg_hba.conf`, `chown 70:70`, `chmod 644`.
     - Exits after finishing, no `restart`.
   - Service `postgres_ssl`:
     - `depends_on: ssl-init: condition: service_completed_successfully`.
     - Mounts `postgres_ssl_certs:/var/lib/postgresql/certs:ro` and `postgres_ssl_conf:/var/lib/postgresql/conf:ro`.
     - `hba_file=/var/lib/postgresql/conf/pg_hba.conf`; other `-c` flags, env, healthcheck and labels unchanged.
   - Top-level `volumes`: `postgres_data_176_ssl`, `postgres_ssl_certs`, `postgres_ssl_conf`.
   - Comment block at the top: self-contained, requires Compose ≥ 2.23.1, and `docker run --rm -v postgres-ssl_postgres_ssl_certs:/c alpine cat /c/ca.crt > ca.crt` to extract the CA.
2. Delete `recipes/postgres/generate-certs.sh` and `recipes/postgres/pg_hba.conf`.
3. Remove the `recipes/postgres/ssl_certs/` line from `.gitignore`.
4. Validate (see Unit Tests below).
5. Update requirement/plan statuses and index on completion.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `recipes/docker-compose.postgres-ssl.5432.yml` | Modify | Inline configs, init service, named volumes |
| `recipes/postgres/generate-certs.sh` | Delete | Inlined into compose |
| `recipes/postgres/pg_hba.conf` | Delete | Inlined into compose |
| `.gitignore` | Modify | Drop `recipes/postgres/ssl_certs/` |
| `requirements/_index.md`, REQ-115 files | Modify | Status updates |

## Key Code Snippets

Escaping rules for inline `content:` and `command:`:

- Compose interpolates `$VAR` in `content:`, so every shell `$` is written `$$` (e.g. `$$CERT_DIR`, `$$(...)` if used).
- The Portainer template parser (`internal/portainer/templates.go`, `envVarRe`) matches `${NAME}`. A `$${NAME}` would be picked up as a spurious template env var, so the embedded script must use the **brace-less** form (`$$CERT_DIR`, `$$SERVER_CERT`) and no `${...}` except the real `POSTGRES_*` variables.
- The script's `-extfile /dev/stdin <<EOF` heredoc is replaced with `-extfile /tmp/san.cnf` (written with `printf`) to avoid a nested heredoc, and `$` free content.
- Config mounts use long syntax (`source`, `target`, `mode: 0444`).

```yaml
configs:
  generate_certs:
    content: |
      #!/bin/sh
      set -e
      CERT_DIR=/certs
      ...
  pg_hba:
    content: |
      local   all  all              scram-sha-256
      hostssl all  all  0.0.0.0/0   scram-sha-256
      hostssl all  all  ::/0        scram-sha-256
```

## Unit Tests

No Go changes. Validation is manual (Docker and Compose are available in this environment; if the daemon is unusable, `config` checks only).

| Test | Input | Expected Output |
|------|-------|-----------------|
| Compose lint | `docker compose -f <file> config` from an empty temp dir containing only the file | Valid, no unresolved references, no bind mounts |
| Fresh start | `up -d` from empty dir | `ssl-init` exits 0, postgres healthy |
| SSL connect | `psql "sslmode=require"` | Connects |
| Non-SSL rejected | `psql "sslmode=disable"` over TCP | `no pg_hba.conf entry` rejection |
| Idempotent | Second `up` | Log "already exist and are valid. Skipping generation." |
| Key perms | `stat` server.key in volume | mode 600, uid 70 |
| Template parse | Run Go portainer tests / inspect parsed env vars for this recipe | Only `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` |
| Repo checks | `go test ./...`, `golangci-lint run` in `lazy-tcp-proxy/` | Pass (unchanged code) |

## Risks & Open Questions

- Requires Compose ≥ 2.23.1 for inline `content:`; older versions fail on `configs`. Documented in the file header.
- Renaming the init service/volumes means existing deployments get fresh certs and the old `postgres_data_176_ssl` data volume is untouched.
- Config mount ownership/mode support differs between Compose versions; the script copies the file rather than relying on mount ownership.
