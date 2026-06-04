# Portainer Git HTTP Endpoint (`/portainer/git`)

**Date Added**: 2026-06-04
**Priority**: Medium
**Status**: Planned

## Problem Statement

Portainer App Templates v2 (type 3 — Docker Compose stack) requires a `repository.url` pointing to a cloneable git repository. Serving inline `compose` content is not supported by Portainer. Users with local recipe files (e.g. at `/etc/lazy-tcp-proxy/recipes/`) have no git repository to point to, so the existing `/portainer/templates` endpoint cannot be used to deploy stacks from the Portainer UI.

## Functional Requirements

1. The proxy exposes a git Smart HTTP read-only endpoint at `/portainer/git` (and sub-paths) on the existing web server.
2. `GET /portainer/git/info/refs?service=git-upload-pack` — returns a pkt-line encoded ref advertisement containing the dynamically computed HEAD commit SHA.
3. `POST /portainer/git/git-upload-pack` — accepts a pkt-line encoded "want" request and responds with a PACK file containing all git objects needed to reconstruct the recipe directory tree.
4. The git repository is generated entirely in-memory from the current contents of `RECIPES_DIR` on every request — no persistent `.git` directory or state is stored on disk.
5. The repository contains a single flat tree: one blob per `*.yml` file in `RECIPES_DIR`, one tree object, one commit object, one HEAD ref (`refs/heads/main`).
6. The `/portainer/templates` endpoint (renamed from `/portainer`) is updated so each template's `repository` field points back to the proxy:
   ```json
   {
     "repository": {
       "url": "http://<host>/portainer/git",
       "stackfile": "<filename>.yml"
     }
   }
   ```
   where `<host>` is derived from the incoming request's `Host` header.
7. The existing `/portainer` path is renamed to `/portainer/templates`.

## User Experience Requirements

- In Portainer: **Settings → App Templates → URL** → `http://<proxy-host>:8080/portainer/templates`
- Each recipe appears in the App Templates list. Clicking deploy causes Portainer to clone `http://<proxy-host>:8080/portainer/git` and read the relevant stackfile.
- Env vars auto-detected from the recipe YAML are shown as editable fields in the deploy wizard.

## Technical Requirements

- Implementation lives in a new file `lazy-tcp-proxy/internal/portainer/git.go` (alongside the existing `templates.go`).
- No external dependencies; stdlib only (`crypto/sha1`, `compress/zlib`, `encoding/binary`, `fmt`, `sort`).
- Git object format: `"<type> <size>\0<content>"` → SHA1 hashed; content zlib-compressed in PACK.
- PACK file format: 4-byte magic (`PACK`), 4-byte version (`2`), 4-byte object count, then per-object entries (type/size varint + zlib-compressed content), then 20-byte SHA1 checksum of the entire PACK.
- pkt-line format: 4 hex-digit length prefix (including the 4 bytes themselves) + data; `0000` = flush packet.
- Delta compression is NOT used — all objects are stored as `OBJ_BLOB` (3), `OBJ_TREE` (2), `OBJ_COMMIT` (1).
- Tree object entries are sorted by filename (git requires lexicographic order).
- The commit object uses a fixed author/timestamp (`lazy-tcp-proxy <noreply@localhost>`, epoch 0) so the commit SHA is deterministic for identical file contents — making the endpoint cache-friendly.
- Serve both `info/refs` and `git-upload-pack` from the same `http.Handler` registered at `GET /portainer/git/` in the mux.

## Acceptance Criteria

- [ ] `git clone http://localhost:8080/portainer/git` successfully clones a repo containing all `*.yml` files from `RECIPES_DIR`.
- [ ] After adding/removing a recipe file, a fresh `git clone` reflects the change (no server restart needed).
- [ ] `GET /portainer/templates` returns each template with a `repository.url` pointing to `/portainer/git` and `repository.stackfile` set to the filename.
- [ ] The `compose` and `logo` and `description` fields are removed from the template response (replaced by `repository`).
- [ ] `GET /portainer` (old path) returns 404.
- [ ] Portainer can successfully deploy a stack using the App Templates endpoint.

## Dependencies

- REQ-097 (Portainer App Templates Endpoint) — this replaces the `compose` inline approach from that requirement.

## Implementation Notes

- The `Host` header approach for building the `repository.url` means the URL is always correct whether the proxy is accessed directly or via a reverse proxy, as long as the `Host` header is set correctly.
- If `RECIPES_DIR` is empty, `GET /portainer/git/info/refs` should still return a valid (empty repo) response rather than erroring.
- Portainer sends a `want <sha>` line followed by `done`. The server responds with `NAK\n` then the PACK data — this is the "no common base" path of the protocol (full clone).
