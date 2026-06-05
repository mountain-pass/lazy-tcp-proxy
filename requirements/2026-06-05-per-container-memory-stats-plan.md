# Per-Container Memory Stats in /status Endpoint — Implementation Plan

**Requirement**: [2026-06-05-per-container-memory-stats.md](2026-06-05-per-container-memory-stats.md)
**Date**: 2026-06-05
**Status**: Draft

## Implementation Steps

1. **Define `ContainerMemoryStat` struct in `main.go`** — add a small exported-enough struct (package-level) for the per-container entry used in the interface and JSON output:
   ```go
   type ContainerMemoryStat struct {
       Name        string `json:"name"`
       MemoryUsed  int64  `json:"memory_used"`
       MemoryLimit int64  `json:"memory_limit"`
   }
   ```

2. **Add `ContainerMemoryStats` to the `backendManager` interface in `main.go`** — replaces the current `MemoryStats` signature with a richer return that includes per-container data. Remove the old `MemoryStats` method from the interface (it has exactly one call site in `runStatusServer`).
   ```go
   ContainerMemoryStats(ctx context.Context) (used, total int64, containers []ContainerMemoryStat, err error)
   ```

3. **Implement `ContainerMemoryStats` on the Docker `Manager` in `internal/docker/manager.go`** — extend the existing `MemoryStats` goroutine fan-out to also capture `container.Name` and `MemoryStats.Limit` per container, building the `[]ContainerMemoryStat` slice alongside the existing `used` accumulator. The existing `MemoryStats` method is replaced by this new one (or delegates to it).

   Key detail: Docker container names are `[]string` like `["/my-postgres"]`; strip the leading `/` from the first element.

   `MemoryLimit`: use `s.MemoryStats.Limit`; if it is 0 or equals `total` (host RAM), keep the raw value — callers receive both `memory_limit` per container and `memory_total` globally so they can decide.

4. **Update the Kubernetes backend stub in `internal/k8s/backend.go`** — rename `MemoryStats` to `ContainerMemoryStats`, add `[]ContainerMemoryStat` to the return, return `nil` for that slice.

5. **Update `runStatusServer` in `main.go`** — call `mgr.ContainerMemoryStats(r.Context())` instead of `mgr.MemoryStats`. When the call succeeds, populate `memory_used`, `memory_total`, and `containers` in the JSON map. Use a `*[]ContainerMemoryStat` (pointer to slice) so that when containers is `nil` the key is omitted via `omitempty`-style logic, or use the same conditional-assignment pattern already used for `memUsed`/`memTotal`.

   Since `json.Marshal` encodes a `nil` slice as `null` (not absent), use a `*[]ContainerMemoryStat` and only set it when the slice is non-nil and len > 0, consistent with how `memUsed` / `memTotal` use `*int64`.

   ```go
   used, total, perContainer, err := mgr.ContainerMemoryStats(r.Context())
   if err == nil {
       memUsed = &used
       memTotal = &total
       if len(perContainer) > 0 {
           memContainers = &perContainer
       }
   }
   // JSON map key: "containers": memContainers  (omitted when nil)
   ```

6. **Remove the old `MemoryStats` method** from `Manager` (Docker) and from `Backend` (K8s) — it is fully superseded.

7. **Run `go build ./...`, `golangci-lint run`, `go test ./...`** inside `lazy-tcp-proxy/` and fix any issues.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/main.go` | Modify | Add `ContainerMemoryStat` struct; replace `MemoryStats` with `ContainerMemoryStats` in interface and `runStatusServer` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Replace `MemoryStats` with `ContainerMemoryStats`; capture per-container name + limit in fan-out |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Rename stub to `ContainerMemoryStats`; add nil containers return |

## API Contracts

`GET /status` — augmented response shape:

```json
{
  "services": [ ... ],
  "memory_used": 1234567,
  "memory_total": 8589934592,
  "containers": [
    { "name": "my-postgres", "memory_used": 52428800, "memory_limit": 536870912 },
    { "name": "my-redis",    "memory_used": 10485760, "memory_limit": 8589934592 }
  ]
}
```

- `containers` is absent when Docker stats are unavailable (not `null`).
- `memory_limit` is the Docker-reported limit in bytes (0 means unlimited/host RAM).

## Key Code Snippets

`ContainerMemoryStats` goroutine fan-out (replacing the body of `MemoryStats`):

```go
type perContainerStat struct {
    name  string
    used  int64
    limit int64
}
var stats []perContainerStat
var mu sync.Mutex
var wg sync.WaitGroup
for _, c := range containers.Items {
    wg.Add(1)
    go func(id string, names []string) {
        defer wg.Done()
        result, statErr := m.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false})
        if statErr != nil { return }
        defer result.Body.Close() //nolint:errcheck
        var s container.StatsResponse
        if decErr := json.NewDecoder(result.Body).Decode(&s); decErr != nil { return }
        memUsage := s.MemoryStats.Usage
        if cache, ok := s.MemoryStats.Stats["inactive_file"]; ok {
            if cache < memUsage { memUsage -= cache }
        }
        name := id // fallback
        if len(names) > 0 { name = strings.TrimPrefix(names[0], "/") }
        mu.Lock()
        used += int64(memUsage) //nolint:gosec
        stats = append(stats, perContainerStat{name: name, used: int64(memUsage), limit: int64(s.MemoryStats.Limit)}) //nolint:gosec
        mu.Unlock()
    }(c.ID, c.Names)
}
wg.Wait()
```

## Unit Tests

No new test files are required — the existing integration tests exercise the `/status` endpoint. Manual verification via `curl /status` in a running environment confirms the new field.

## Risks & Open Questions

- `ContainerList` returns ALL containers visible to the Docker socket, not only those managed by lazy-tcp-proxy. This is consistent with the existing `MemoryStats` behaviour and is intentional — the operator wants a full memory picture.
- `s.MemoryStats.Limit` can equal the host total (Docker sets it to MemTotal when no limit is configured). The raw value is returned; consumers can compare against `memory_total` if they want to detect "no explicit limit".
