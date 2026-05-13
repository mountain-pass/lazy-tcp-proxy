# Dynamic Configuration File (YAML Override Store)

**Date Added**: 2026-05-13
**Priority**: High
**Status**: Planned

## Problem Statement

Docker (and Kubernetes) labels and annotations can only be set when a container/pod is created.
Users with existing, long-running containers cannot change proxy configuration (ports, allow/block
lists, idle timeouts, webhooks, etc.) without destroying and recreating those containers.

Additionally, some proxy targets may not be Docker containers or Kubernetes pods at all — they
could be bare-metal hosts or containers with no `lazy-tcp-proxy` labels — and currently there is
no way to manage these through the proxy.

## Functional Requirements

1. **YAML config file** — a structured file on disk that defines per-service proxy configuration.
   - Path configurable via `CONFIG_PATH` env var.
   - Default path: `/etc/lazy-tcp-proxy/config.yaml`.
   - If the file does not exist at startup, create an empty placeholder and log the action.

2. **YAML wins** — for any container/pod that appears in both the backend labels/annotations and
   the YAML file (matched by `name`), the YAML config takes **full precedence** for the entire
   service entry. Label/annotation values are completely disregarded for that container.

3. **New static targets** — the YAML config may define services with a `host` field that point
   to non-container targets (bare-metal hosts, remote IPs, etc.).
   Static targets are always treated as "running" — the proxy forwards directly without starting
   any container.

4. **Admin HTTP server** — a second HTTP server on a dedicated port exposes a small management API.
   - Port configurable via `ADMIN_PORT` env var (default: `8081`; set to `0` to disable).
   - Protected by an API key supplied via `ADMIN_API_KEY` env var.
   - If `ADMIN_PORT` is non-zero and `ADMIN_API_KEY` is empty, the server refuses to start and
     logs an error.

5. **API endpoints**:

   | Method | Path | Description |
   |--------|------|-------------|
   | `GET`  | `/config` | Return the current in-memory config as JSON |
   | `GET`  | `/config/reload` | Re-read the YAML file from disk and re-apply to running proxy |
   | `PUT`  | `/config/update` | Accept a JSON body, overwrite the YAML file, and re-apply |

6. **Config loaded at startup** — the YAML file is applied immediately after the first backend
   `Discover()` call, before any proxy listeners are opened.

7. **Applies to all backends** — Docker and Kubernetes. Docker Swarm scale-to-zero support is
   a separate future requirement (it requires a dedicated Swarm backend).

## User Experience Requirements

- Users edit the YAML file directly on disk, then call `GET /config/reload` to apply changes.
- Users can also push a new config via `PUT /config/update` (e.g. from a script or CI pipeline)
  without touching the file system directly.
- The admin port is a separate Docker port mapping that users can choose not to expose, giving
  them network-level access control at no extra cost.
- Invalid config entries are rejected with a descriptive error; the proxy continues running with
  the last good config.

## Technical Requirements

- Config file format: **YAML**.
- Admin API transport: **JSON** (request and response bodies).
- API key passed via `X-API-Key` HTTP header.
- Config reload is synchronous from the caller's perspective (the HTTP response returns only after
  the proxy has been updated).
- Static targets (`host` field set) skip the container-start logic and are assumed always
  reachable.
- `name` is the join key between YAML entries and backend-discovered containers (matches
  `ContainerName` for Docker, pod/service name for Kubernetes).
- Port mappings use the same `"listen:target"` string format as Docker labels (e.g. `"9000:80"`).

## YAML Schema

```yaml
# /etc/lazy-tcp-proxy/config.yaml
services:
  - name: "my-container"          # required; matches container/pod name
    host: "192.168.1.100"         # optional; static target — bypasses container start logic
    ports:
      - "9000:80"
    udp_ports:
      - "5353:53"
    allow_list:
      - "192.168.0.0/24"
    block_list:
      - "10.0.0.1"
    idle_timeout_secs: 60
    start_timeout_secs: 30
    webhook_url: "https://example.com/hook"
    dependants:
      - "other-service"
    cron_start: "0 9 * * 1-5"
    cron_stop:  "0 17 * * 1-5"
    http_healthcheck: "http://{{container}}:8080/health"
```

All fields except `name` are optional. When a `name` matches a backend-discovered container,
the entire YAML entry replaces that container's label-derived config (no label fallthrough).

## Acceptance Criteria

- [ ] If `CONFIG_PATH` file does not exist, proxy creates an empty placeholder and logs a warning.
- [ ] `CONFIG_PATH` env var overrides the default `/etc/lazy-tcp-proxy/config.yaml`.
- [ ] YAML config is applied after first `Discover()` at startup.
- [ ] A container present in both labels and YAML: YAML entry fully replaces label-derived config (no fallthrough).
- [ ] A service with only a YAML entry (no Docker label / K8s annotation) is registered and proxied.
- [ ] A static-host (`host:`) YAML entry never triggers a container start; connections are forwarded immediately.
- [ ] `GET /config` returns current config as JSON (requires valid `X-API-Key` header).
- [ ] `GET /config/reload` re-reads YAML, re-applies, and returns success/error JSON (requires auth).
- [ ] `PUT /config/update` accepts JSON, writes YAML file, reloads, and returns success/error JSON (requires auth).
- [ ] Requests with missing/wrong `X-API-Key` receive `401 Unauthorized`.
- [ ] Admin server does not start when `ADMIN_PORT=0`.
- [ ] Admin server logs an error and exits if `ADMIN_PORT` > 0 and `ADMIN_API_KEY` is empty.
- [ ] Invalid YAML/JSON on `PUT /config/update` returns `400 Bad Request` with a description; existing config is unchanged.
- [ ] Invalid service entry (e.g. bad port mapping) is rejected per-entry with a log warning; valid entries are still applied.
- [ ] Port strings use `"listen:target"` format, consistent with Docker labels.

## Dependencies

- Depends on: REQ-001 (core proxy), REQ-025 (HTTP status endpoint), REQ-038/REQ-049 (K8s backend)
- Future: Docker Swarm scale-to-zero (separate requirement — needs dedicated Swarm backend)
- Affects: `main.go`, `internal/types/types.go`, `internal/proxy/server.go`, `internal/docker/manager.go`, `internal/k8s/backend.go`

## Implementation Notes

- New package: `internal/config` — YAML store, merge logic, validation.
- New package: `internal/admin` — admin HTTP server, auth middleware, handlers.
- `TargetInfo` gains a `StaticHost string` field; when non-empty, `ProxyServer` resolves the target
  address directly rather than asking the backend to start a container.
- Merge order: (1) discover from backend → (2) for each YAML entry, if `name` matches a discovered
  container replace it entirely; if no match, add as new entry → (3) call `srv.Update()`.
- The admin server's reload handler re-runs steps 1–3 (calls `Discover()` then applies config).
- Existing `ParsePortMappings()` in `internal/types/types.go` can be reused for YAML port strings.
