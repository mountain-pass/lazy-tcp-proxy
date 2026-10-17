# Include Stopped Containers' Allocated Memory — Implementation Plan

**Requirement**: [2026-06-06-stopped-container-memory-allocation.md](2026-06-06-stopped-container-memory-allocation.md)
**Date**: 2026-06-06
**Status**: Completed

## Implementation Steps

1. **Add `Running` field to `types.ContainerMemoryStat`** (`internal/types/types.go:82`):
   ```go
   type ContainerMemoryStat struct {
       Name        string `json:"name"`
       MemoryUsed  int64  `json:"memory_used"`
       MemoryLimit int64  `json:"memory_limit"`
       Running     bool   `json:"running"`
   }
   ```

2. **Update `ContainerMemoryStats` in `internal/docker/manager.go:1157`**:
   - Change `m.cli.ContainerList(ctx, client.ContainerListOptions{})` to `client.ContainerListOptions{All: true}`.
   - In the per-container goroutine, branch on `c.State == container.StateRunning` (or compare to `"running"` if no constant is exported — check `container.ContainerState` for available constants):
     - **Running**: keep existing `ContainerStats` fan-out logic unchanged (usage = `Usage - inactive_file`, limit = `MemoryStats.Limit`, contributes to aggregate `used`).
     - **Not running**: call `m.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})`; set `MemoryUsed: 0`, `MemoryLimit: inspectResult.Container.HostConfig.Memory` (falling back to `total`, the host's total memory, when `HostConfig.Memory == 0`), `Running: false`. Do NOT add to aggregate `used`.
   - Set `Running: true` for the running branch.
   - Guard nil `HostConfig` (defensive — Inspect should always populate it for real containers, but check before dereferencing).

3. **No changes needed to `main.go` or the K8s backend** — the interface signature and JSON wiring (`memContainers`) already pass through `[]types.ContainerMemoryStat`; the new `running` field flows automatically through `json.Marshal`.

4. **Run `go build ./...`, `golangci-lint run`, `go test ./...`** inside `lazy-tcp-proxy/` and fix any issues.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/types/types.go` | Modify | Add `Running bool` field to `ContainerMemoryStat` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | List all containers; branch running vs stopped; inspect stopped containers for configured memory limit |

## API Contract Change

`GET /status` — `containers` entries gain a `running` field and now include stopped containers:

```json
{
  "containers": [
    { "name": "my-postgres", "memory_used": 52428800, "memory_limit": 536870912, "running": true },
    { "name": "my-idle-app", "memory_used": 0,         "memory_limit": 268435456, "running": false },
    { "name": "my-unlimited-app", "memory_used": 0,     "memory_limit": 8589934592, "running": false }
  ]
}
```

## Risks & Open Questions

- Extra `ContainerInspect` calls are made only for non-running containers (typically a small subset), so the added Docker API load is minimal.
- `container.ContainerState` constant name needs verifying in the vendored `moby/moby` API version (fall back to string comparison `string(c.State) == "running"` if no constant exists).
