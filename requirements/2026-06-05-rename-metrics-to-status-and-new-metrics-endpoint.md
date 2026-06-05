# Rename /metrics → /status and New /metrics Hourly-Activity Endpoint

**Date Added**: 2026-06-05
**Priority**: Medium
**Status**: Planned

## Problem Statement

1. The current `/metrics` endpoint (service list + memory) is more accurately described as a "status" endpoint. The name `/metrics` should be reserved for time-series data useful for dashboards.
2. The `memory_used` and `memory_total` fields currently report Go-runtime heap statistics from inside the proxy container. They do not reflect the true memory picture across all running Docker containers or the total memory available to the Docker host.
3. There is no endpoint that exposes historical service-usage data from the PostgreSQL metrics store, which is needed to determine which services were active and when (prerequisite for a usage-history web page).

## Functional Requirements

### 1. Rename `/metrics` → `/status`
- The existing `/metrics` endpoint is renamed to `/status`.
- The JSON response shape remains the same except for the two memory fields (see below).

### 2. Docker-level memory fields on `/status`
- Replace the Go-runtime `memory_used` / `memory_total` fields with values obtained via the Docker socket:
  - `memory_used`: sum of the memory working set (RSS / cache) for **all running containers** as reported by `/containers/{id}/stats?stream=false`.
  - `memory_total`: total physical memory available to the Docker host, obtained from `GET /info` (`MemTotal`).
- If the Docker socket is unavailable or returns an error, fall back to `null` for both fields rather than crashing.
- Both values are in bytes (integers).

### 3. New `/metrics` endpoint — hourly activity matrix
- Available only when `METRICS_POSTGRES_URL` is configured. Returns `404` (or an empty body with a descriptive message) when metrics are disabled.
- Queries `proxy_metrics` for the last 7 days (168 hours), grouping rows into 1-hour buckets using `date_trunc('hour', rollup_at)`.
- For each `(container_name, port, is_udp)` tuple, emits one entry per hour bucket that has **at least one row where `uptime_ms_total > 0`**.
- Response shape (JSON array):

```json
[
  {
    "container_name": "pihole",
    "port": 53,
    "is_udp": true,
    "hour": "2026-06-05T10:00:00Z",
    "active": true
  },
  ...
]
```

- `hour` is an RFC3339 UTC timestamp truncated to the hour.
- `active` is always `true` in the result set (only active hours are returned — inactive hours are omitted to keep the payload small).
- Results are ordered by `container_name`, `port`, `is_udp`, `hour`.

## User Experience Requirements

- The `/status` URL must continue to work for existing consumers (the HTML dashboard calls this endpoint).
- The new `/metrics` endpoint is consumed by a forthcoming web page to render a usage heatmap/calendar.
- No breaking changes to `/traefik`, `/health`, or any other existing endpoint.

## Technical Requirements

- Docker memory fetch is best-effort: spawn a goroutine per container (or use the existing Docker manager's client), collect results with a short timeout (e.g. 3 seconds total), sum them, and return.
- The Docker manager (`internal/docker/manager.go`) already holds a `*client.Client`. Expose a `MemoryStats(ctx) (used, total int64, err error)` method on the Docker backend (no-op / zeros on Kubernetes).
- The `backendManager` interface in `main.go` gains a `MemoryStats(ctx context.Context) (used, total int64, err error)` method.
- The PostgreSQL query for `/metrics` uses a single SQL statement with `GROUP BY` and a `HAVING` clause.
- The metrics handler in `runStatusServer` receives a `*metrics.Collector` (or nil) so it can query the database.
- The PostgreSQL query method lives in `internal/metrics` (e.g. `Collector.HourlyActivity`).

## Acceptance Criteria

- [ ] `GET /status` returns the same JSON as the old `GET /metrics` (services list + memory fields), with Docker-sourced memory values.
- [ ] `GET /metrics` with no `METRICS_POSTGRES_URL` → `{"error": "metrics not configured"}` with HTTP 503.
- [ ] `GET /metrics` with `METRICS_POSTGRES_URL` set → JSON array of hourly activity records for the last 7 days.
- [ ] `memory_used` equals the sum of all running containers' memory usage (bytes) from the Docker stats API.
- [ ] `memory_total` equals the Docker host's `MemTotal` from `GET /info`.
- [ ] No regression in existing `/traefik`, `/health`, `/`, `/portainer/*` endpoints.
- [ ] HTML dashboard (which fetches `/status`) continues to work.
- [ ] Lint (`golangci-lint run`) passes.
- [ ] Tests (`go test ./...`) pass.

## Dependencies

- Requires `METRICS_POSTGRES_URL` (REQ-094) for the new `/metrics` endpoint.
- Relates to REQ-095 (previous rename of `/status` → `/metrics`); this requirement partially reverts that rename and re-purposes the `/metrics` path.
- The forthcoming usage-history web page will consume the new `/metrics` endpoint.

## Implementation Notes

- The Docker `ContainerStats` API returns a JSON blob with a `memory_stats` object. The usable field is `memory_stats.usage - memory_stats.stats.cache` (or `memory_stats.usage` on older kernels where `cache` is absent). On cgroups v2, use `memory_stats.usage`.
- Fetching stats for many containers in parallel is fine; use `context.WithTimeout` to bound the overall latency.
- The `backendManager` interface change requires a stub in `backend_k8s.go` and `backend_docker.go`.
