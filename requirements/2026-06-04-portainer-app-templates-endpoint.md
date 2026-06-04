# Portainer App Templates Endpoint (`/portainer`)

**Date Added**: 2026-06-04
**Priority**: Medium
**Status**: Planned

## Problem Statement

Portainer supports a custom "App Templates" endpoint that lets users provision Docker stacks directly from the Portainer UI. Lazy-TCP-Proxy ships a library of ready-made Docker Compose recipes in the `recipes/` directory. Currently users must manually copy these files to provision services. Exposing them as a Portainer-compatible template list lets Portainer users deploy managed services in a few clicks.

## Functional Requirements

1. The proxy exposes a new HTTP endpoint `GET /portainer` on the existing web server (same port as `/`, `/metrics`, `/traefik`).
2. The response is a JSON object in Portainer App Templates v2 format:
   ```json
   {
     "version": "2",
     "templates": [ ... ]
   }
   ```
3. Every file matching `recipes/docker-compose.*.yml` is included as a separate template entry.
4. Each template entry contains:
   - `type`: `3` (Portainer stack type for Docker Compose)
   - `title`: derived from the recipe filename (e.g. `docker-compose.postgres.5432.yml` → `postgres`)
   - `description`: empty string (no metadata file needed for MVP)
   - `logo`: empty string
   - `compose`: the raw contents of the recipe YAML file (inline, not a URL)
   - `env`: auto-detected list of environment variable substitutions found in the YAML, in Portainer env format (see below)
5. Auto-detection of env vars: scan each recipe file for `${VAR_NAME:-default}` and `${VAR_NAME}` patterns, deduplicate, and emit one env entry per unique variable:
   ```json
   { "name": "VAR_NAME", "label": "VAR_NAME", "default": "default" }
   ```
   - If the pattern has no default (e.g. `${VAR_NAME}`), omit the `default` field.
6. The endpoint requires no authentication — it is public, matching the behaviour of `/` (the status dashboard).
7. The endpoint is registered on the existing web `http.ServeMux` in `main.go`, consistent with `/traefik` and `/metrics`.

## User Experience Requirements

- Portainer users configure the endpoint URL (e.g. `http://<proxy-host>:8080/portainer`) in Portainer → Settings → App Templates URL.
- On next page load, all recipes appear in the Portainer "App Templates" list.
- Each template shows its configurable variables (env vars) as editable fields in the Portainer deploy wizard.

## Technical Requirements

- Recipes directory path defaults to `recipes/` relative to the working directory, overridable by the `RECIPES_DIR` environment variable.
- If the recipes directory does not exist or is empty, return a valid but empty templates list (`{"version":"2","templates":[]}`).
- Response `Content-Type` must be `application/json`.
- Regex for env var detection: `\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}` — captures name and optional default.
- Implementation lives in a new file `lazy-tcp-proxy/internal/portainer/templates.go` (handler + parser), keeping `main.go` as the wiring point only.
- No new external dependencies; use stdlib only.

## Acceptance Criteria

- [ ] `GET /portainer` returns HTTP 200 with `Content-Type: application/json`.
- [ ] Response is valid Portainer App Templates v2 JSON (`version: "2"`, `templates` array).
- [ ] Each recipe file in `recipes/` produces exactly one template entry.
- [ ] `title` is correctly derived from the recipe filename (service name portion only).
- [ ] `compose` field contains the full, unmodified YAML content of the recipe file.
- [ ] `env` array contains one entry per unique `${VAR}` / `${VAR:-default}` substitution found in the YAML.
- [ ] Env entries with a default value include the `default` field; those without do not.
- [ ] If `RECIPES_DIR` is set, recipes are loaded from that path instead of `./recipes`.
- [ ] If the recipes directory is missing or empty, the endpoint returns `{"version":"2","templates":[]}`.
- [ ] No authentication is required to access `/portainer`.
- [ ] Existing endpoints (`/`, `/metrics`, `/traefik`) are unaffected.

## Dependencies

- REQ-025 (HTTP Status Endpoint) — establishes the web server that this endpoint extends
- REQ-054 (Docker Compose Recipes) — the recipe files being served
- REQ-069 (Traefik Integration) — pattern precedent for adding a new JSON endpoint to the web server

## Implementation Notes

- Portainer App Templates v2 spec: `type: 3` = Docker Compose stack (as opposed to `1` = container, `2` = Swarm).
- The `name` field on a Portainer template is deprecated in v2; `title` is used instead.
- Recipe filenames follow the pattern `docker-compose.<service>.<ports>.yml`; the title should extract `<service>` only (e.g. `postgres`, `ollama-cpu`).
- Env var scanning should operate on raw text (not parsed YAML) to avoid dependencies on a YAML library.
