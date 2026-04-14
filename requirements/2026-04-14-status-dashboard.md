# Status Dashboard (HTML UI at /)

**Date Added**: 2026-04-14
**Priority**: Low
**Status**: Completed

## Problem Statement

The root path `/` previously issued a 301 redirect to `/status`, which returns raw JSON. While functional for `curl`, it provides no human-friendly view of proxy state in a browser. A lightweight, self-contained HTML dashboard served at `/` would let operators quickly see which containers are up, idle, or down without needing external tooling.

## Functional Requirements

- `GET /` serves a self-contained HTML page (no external dependencies).
- The page polls `GET /status` every 2 seconds and updates the view in-place.
- Containers are grouped by `container_id`; multiple port mappings for the same container appear within a single card.
- Each container group displays a status badge derived from its port states:
  - **up** — container is running and has at least one active connection.
  - **idle** — container is running but has no active connections.
  - **down** — container is not running.
  - When a group has mixed-status ports, the best status wins (`up` > `idle` > `down`).
- Port mappings are shown as `:listen_port → :target_port`, with active connection count highlighted when non-zero.
- A "Last updated" timestamp is shown and refreshed after every poll.
- Fetch errors are surfaced inline without crashing the page.
- Any path other than `/` handled by the root catch-all returns HTTP 404.

## User Experience Requirements

- Dark-themed layout with clear visual hierarchy (card per container, colour-coded badges).
- Status badge colours: green (`up`), amber (`idle`), red (`down`).
- No page reloads — DOM is updated in place each refresh cycle.
- Short container ID shown in monospace for identification without dominating the view.

## Technical Requirements

- The HTML is embedded as a Go raw-string constant (`statusDashboardHTML`) in `main.go`.
- The `"/"` handler in `runStatusServer` serves the constant with `Content-Type: text/html; charset=utf-8`.
- No new Go dependencies.
- No new files — the constant lives in `main.go`.

## Acceptance Criteria

- [x] `GET /` returns HTTP 200 with `Content-Type: text/html`.
- [x] The page auto-refreshes container data every 2 seconds.
- [x] Containers with multiple port mappings are grouped into a single card.
- [x] Status badges reflect `up` / `idle` / `down` correctly.
- [x] `GET /status` and `GET /health` are unaffected.
- [x] Unknown paths (e.g. `GET /foo`) return HTTP 404.

## Dependencies

- REQ-025 (HTTP Status Endpoint) — the dashboard consumes `/status`.
- REQ-029 (Root Redirect to /status) — superseded by this requirement.

## Implementation Notes

- The Go `http.ServeMux` `"/"` pattern is a catch-all; an explicit `r.URL.Path != "/"` guard is added to return 404 for sub-paths rather than serving the dashboard unexpectedly.
- Status logic mirrors the JSON fields: `running == false` → down; `running == true && active_conns == 0` → idle; `running == true && active_conns > 0` → up.
- HTML is escaped via a small inline `esc()` helper to prevent XSS from container names or IDs.
