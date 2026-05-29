# Rename /status to /metrics and Add Memory Fields

**Date Added**: 2026-05-29
**Priority**: Medium
**Status**: Planned

## Problem Statement

The `/status` endpoint returns a list of managed services (proxy mappings) as a JSON array. The name `/status` is ambiguous and conflicts with conventions used by upstream services (e.g. Selenium has its own `/status`). Renaming it to `/metrics` better reflects what the endpoint provides: operational data about the proxy. Additionally, the endpoint should expose process memory usage (`memory_used` and `memory_total`) so operators can monitor the proxy's memory footprint without external tooling.

## Functional Requirements

1. Rename the HTTP route `GET /status` to `GET /metrics`.
2. The `/metrics` response changes from a bare JSON array to an object with three top-level keys:
   - `services`: the existing `[]TargetSnapshot` array (unchanged).
   - `memory_used`: current heap bytes in use (`runtime.MemStats.Alloc`), as an integer (bytes).
   - `memory_total`: total memory obtained from the OS (`runtime.MemStats.Sys`), as an integer (bytes).
3. The status dashboard HTML (embedded in `main.go`) fetches `/metrics` instead of `/status` and accesses `data.services` for rendering.
4. All documentation and example files that reference `/status` (in the context of this proxy's own endpoint) are updated to `/metrics`.
5. Requirement files that mention `/status` as the proxy endpoint name are updated.

## User Experience Requirements

- Operators calling `curl http://localhost:8080/status` will receive a 404. They must update to `curl http://localhost:8080/metrics`.
- The dashboard at `/` continues to work automatically (it fetches `/metrics` internally).
- The JSON shape changes: callers that consumed the bare array must wrap their access with `.services`.

## Technical Requirements

- Use `runtime.ReadMemStats` to populate `memory_used` (`MemStats.Alloc`) and `memory_total` (`MemStats.Sys`).
- The Selenium recipe file (`recipes/docker-compose.selenium-chromium.4444,7900.yml`) references Selenium's own `/status` endpoint — this must NOT be changed.
- No backwards-compatibility shim (no redirect from `/status` to `/metrics`).

## Acceptance Criteria

- [ ] `GET /metrics` returns HTTP 200 with `Content-Type: application/json` containing `services`, `memory_used`, and `memory_total`.
- [ ] `GET /status` returns HTTP 404.
- [ ] `memory_used` and `memory_total` are non-zero integers in the response.
- [ ] The dashboard at `/` correctly renders service rows (fetches `/metrics`, uses `data.services`).
- [ ] `README.md` and `README_CONFIG.md` reference `/metrics` instead of `/status`.
- [ ] All example READMEs and compose/YAML files reference `/metrics`.

## Dependencies

- REQ-025 (HTTP Status Endpoint) — superseded by this rename.
- REQ-056 (Status Dashboard) — dashboard JS updated.

## Implementation Notes

- `runtime.MemStats.Alloc` is heap memory currently allocated (in-use objects only).
- `runtime.MemStats.Sys` is the total memory mapped from the OS (includes all Go runtime arenas).
- No other endpoint (`/health`, `/traefik`, `/`) is affected.
