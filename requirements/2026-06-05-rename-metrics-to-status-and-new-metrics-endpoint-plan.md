# Rename /metrics → /status; New /metrics Hourly-Activity Endpoint — Implementation Plan

**Requirement**: [2026-06-05-rename-metrics-to-status-and-new-metrics-endpoint.md](2026-06-05-rename-metrics-to-status-and-new-metrics-endpoint.md)
**Date**: 2026-06-05
**Status**: Implemented

## Implementation Steps

1. Add `HourlyActivity(ctx, db) ([]HourlyActivityRow, error)` query method to `internal/metrics/postgres.go` using a single SQL `GROUP BY date_trunc('hour', rollup_at)` query.
2. Add `HourlyActivity(ctx) ([]HourlyActivityRow, error)` method to `*Collector` in `internal/metrics/collector.go` that delegates to the db.
3. Add `HourlyActivityRow` struct to `internal/metrics/collector.go`.
4. Add `MemoryStats(ctx context.Context) (used, total int64, err error)` to the `backendManager` interface in `main.go`.
5. Implement `MemoryStats` on `*docker.Manager` in `internal/docker/manager.go`: list all running containers, fetch stats in parallel with a 3-second context, sum `memory_stats.usage`, get host total from `Info()`.
6. Add no-op `MemoryStats` stub to `*k8s.Backend` in `internal/k8s/backend.go`.
7. Update `runStatusServer` signature in `main.go` to accept `mgr backendManager` and `metricsCollector *metrics.Collector`.
8. Rename the `/metrics` handler route to `/status`; update it to call `mgr.MemoryStats` for the memory fields.
9. Add a new `/metrics` handler: if `metricsCollector == nil` return 503; otherwise call `HourlyActivity` and return JSON.
10. Update `main()` to pass `mgr` and `collector` to `runStatusServer` (collector may be nil initially; refactor so the server holds a pointer-to-pointer or setter so the collector can be set after network join).
11. Update `html/src/App.svelte` to fetch `/status` instead of `/metrics`.
12. Rebuild Svelte app (`npm run build` in `html/`).
13. Update requirement status to "In Progress" → "Completed".

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/metrics/postgres.go` | Modify | Add `hourlyActivity` SQL query and helper |
| `lazy-tcp-proxy/internal/metrics/collector.go` | Modify | Add `HourlyActivityRow` struct and `HourlyActivity` method |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Add `MemoryStats` method |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Add no-op `MemoryStats` stub |
| `lazy-tcp-proxy/main.go` | Modify | Interface, route rename, new /metrics handler, pass mgr to status server |
| `lazy-tcp-proxy/html/src/App.svelte` | Modify | Update fetch URL `/metrics` → `/status` |
| `lazy-tcp-proxy/html/dist/index.html` | Modify | Rebuild Svelte output |

## API Contracts

### GET /status
```json
{
  "services": [ ... ],
  "memory_used": 123456789,
  "memory_total": 8589934592
}
```
`memory_used` and `memory_total` are integers (bytes) or `null` on error.

### GET /metrics (requires METRICS_POSTGRES_URL)
```json
[
  { "container_name": "pihole", "port": 53, "is_udp": true,  "hour": "2026-06-05T10:00:00Z", "active": true },
  { "container_name": "pihole", "port": 53, "is_udp": true,  "hour": "2026-06-05T11:00:00Z", "active": true }
]
```
Returns HTTP 503 with `{"error":"metrics not configured"}` when `METRICS_POSTGRES_URL` is absent.

## Key Code Snippets

### PostgreSQL query
```sql
SELECT container_name, port, is_udp, date_trunc('hour', rollup_at) AS hour
FROM proxy_metrics
WHERE rollup_at >= NOW() - INTERVAL '7 days'
  AND uptime_ms_total > 0
GROUP BY container_name, port, is_udp, date_trunc('hour', rollup_at)
ORDER BY container_name, port, is_udp, hour
```

### Docker memory fetch
```go
func (m *Manager) MemoryStats(ctx context.Context) (used, total int64, err error) {
    ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()
    info, err := m.cli.Info(ctx)
    if err != nil { return 0, 0, err }
    total = info.MemTotal
    containers, err := m.cli.ContainerList(ctx, container.ListOptions{})
    if err != nil { return 0, total, err }
    var mu sync.Mutex
    var wg sync.WaitGroup
    for _, c := range containers {
        wg.Add(1)
        go func(id string) {
            defer wg.Done()
            resp, err := m.cli.ContainerStats(ctx, id, false)
            if err != nil { return }
            defer resp.Body.Close()
            var s types.StatsJSON
            json.NewDecoder(resp.Body).Decode(&s)
            memUsage := s.MemoryStats.Usage
            if cache, ok := s.MemoryStats.Stats["inactive_file"]; ok { memUsage -= int64(cache) }
            mu.Lock(); used += int64(memUsage); mu.Unlock()
        }(c.ID)
    }
    wg.Wait()
    return used, total, nil
}
```

## Risks & Open Questions

- Docker stats API (`/containers/{id}/stats?stream=false`) can be slow under load; the 3-second context timeout mitigates this.
- On Kubernetes the memory stats are always `null` (no-op stub).
- The `metricsCollector` is initialised after `runStatusServer` is called in `main()`. Solution: pass a `**metrics.Collector` pointer, or expose a `SetCollector` on the status server. The simplest approach is to move `runStatusServer` to after the collector is initialised, or pass a func closure.
