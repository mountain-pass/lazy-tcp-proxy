# Self-Contained Postgres SSL Recipe

**Date Added**: 2026-10-16
**Priority**: Medium
**Status**: Planned

## Problem Statement

`recipes/docker-compose.postgres-ssl.5432.yml` depends on external files in `recipes/postgres/` (`generate-certs.sh`, `pg_hba.conf`) and a host bind-mount (`./postgres/ssl_certs`). The recipe cannot be copied, pasted into Portainer, or served via the `/portainer` templates endpoint on its own — it only works when the whole `recipes/` tree is present alongside it.

## Functional Requirements

1. `docker-compose.postgres-ssl.5432.yml` must work as a single standalone file with no sibling files or host directories.
2. The `pg_hba.conf` content (SSL-only TCP, scram-sha-256 on the local socket) must be defined inside the compose file.
3. The cert-generation logic (CA + server cert, SANs `postgres`, `localhost`, `127.0.0.1`, skip if valid for >30 days, regenerate when expiring, key mode 600, ownership uid/gid 70) must be defined inside the compose file.
4. Certificates must persist in a named volume (replacing the `./postgres/ssl_certs` bind mount) and be shared read-only with the postgres container.
5. Generated behaviour must be identical to today's recipe: postgres starts with `ssl=on`, the same cert/key/CA paths and the custom `hba_file`; the `lazy-tcp-proxy.*` labels and healthcheck are unchanged.
6. `recipes/postgres/` (script, `pg_hba.conf`) and the `.gitignore` entry for `recipes/postgres/ssl_certs/` are removed once nothing references them.

## User Experience Requirements

- Users can `docker compose -f docker-compose.postgres-ssl.5432.yml up` from any directory, or paste the file into Portainer, with no prior setup.
- `docker compose down -v` fully cleans up (certs live in a named volume, not on the host).

## Technical Requirements

- Init flow uses `depends_on: condition: service_completed_successfully`, as requested: an init service writes files into named volumes, postgres mounts them.
- Two file-producing init steps (certs, `pg_hba.conf`) may be one service or two; both must complete before postgres starts.
- **Embedding mechanism (decision needed — see Open Questions):**
  - **A. Compose `configs` with inline `content:`** (Compose spec, requires Docker Compose ≥ 2.23.1). The script and `pg_hba.conf` are declared as top-level configs and mounted into the init container; no heredocs in `command`. Cleanest to read.
  - **B. Inline `command`/`entrypoint` in the init service(s)** using a shell heredoc that writes the files. Works on any Compose version; `$` must be escaped as `$$` and the YAML is noisier.
- `pg_hba.conf` must end up readable by postgres at a path outside `PGDATA` (as today, `/var/lib/postgresql/pg_hba.conf`), via a named volume written by the init container.
- The postgres key file must remain `0600` and owned by uid 70 (alpine image); the cert volume is mounted read-only into postgres.
- Cert generation must stay idempotent across `up` runs.
- Update README/recipe docs if they reference the `postgres/` helper directory (none found in a first grep, to be confirmed in Plan).

## Acceptance Criteria

- [ ] The recipe file has no bind mounts and no references to files outside itself.
- [ ] From an empty directory containing only the compose file, `docker compose up` starts postgres with SSL enabled.
- [ ] `psql "sslmode=require"` connects; a non-SSL (`sslmode=disable`) TCP connection is rejected by `pg_hba.conf`.
- [ ] A second `up` does not regenerate valid certs; certs expiring in <30 days are regenerated.
- [ ] `server.key` is `0600` and owned by uid 70 inside the postgres container.
- [ ] `recipes/postgres/` and the obsolete `.gitignore` line are removed.
- [ ] The recipe still loads correctly via the `/portainer` template/recipe handling.

## Dependencies

- Related: REQ-097 (Portainer App Templates Endpoint) — self-contained recipes are a better fit for that endpoint.
- Related: REQ-098 (RECIPES_DIR handling).

## Open Questions

1. **Option A (`configs` inline content, Compose ≥ 2.23.1) or Option B (heredoc in `command`)?** Recommendation: A, unless you need older Compose support.
2. Should existing certs in `recipes/postgres/ssl_certs/` be migrated? Recommendation: no — the new named volume generates fresh certs (clients using the old CA must re-trust the new one).
3. Should the CA be exposed for clients to download (e.g. a documented `docker cp`/volume path)? Currently it isn't; recommendation: keep as is, add a one-line comment on how to extract `ca.crt`.

## Implementation Notes

To be completed in the Plan phase.
