# Overall "/" Root Drive Disk Usage in /status Endpoint — Implementation Plan

**Requirement**: [2026-06-06-disk-usage-stats.md](2026-06-06-disk-usage-stats.md)
**Date**: 2026-06-06
**Status**: Draft

## Implementation Steps

1. **Add a `diskUsage()` helper function in `main.go`** (near the `/status` handler, not on `backendManager`). It calls `unix.Statfs("/", &stat)` (from the already-vendored `golang.org/x/sys/unix`) and returns `(used, total int64, ok bool)`:
   - `total = int64(stat.Blocks) * stat.Bsize`
   - `used = total - int64(stat.Bavail) * stat.Bsize`
   - `ok = false` if `Statfs` returns an error (so the caller can omit the fields)
   - Rationale for NOT adding this to `backendManager`: disk usage of `/` is a property of the host/process the proxy runs on, identical regardless of whether the Docker or Kubernetes backend is active. Adding it to the interface would force a meaningless duplicate stub in `internal/k8s/backend.go`.

2. **Wire into the `/status` handler** (`main.go` ~lines 243–259): alongside the existing `memUsed`/`memTotal` `*int64` pointer locals, add `diskUsed`/`diskTotal *int64`. Call `diskUsage()`; if `ok`, set both pointers; otherwise leave `nil`. Add `"disk_used": diskUsed` and `"disk_total": diskTotal` to the `map[string]any` passed to `enc.Encode`.

3. **Frontend — `html/src/App.svelte`**: mirror the existing memory wiring (lines ~8–9, ~55–56, ~134–141):
   - Add `let diskUsed = $state(0);` / `let diskTotal = $state(0);` (or equivalent existing reactive pattern) and read `data.disk_used ?? 0` / `data.disk_total ?? 0`.
   - Render a second `<MemoryBar used={diskUsed} limit={diskTotal} barWidth="w-64" />` (reusing the existing component — it's generic over bytes, not memory-specific) next to/below the existing memory bar, guarded by `diskTotal > 0`. Add a label distinguishing "Memory" vs "Disk" if the component or surrounding markup doesn't already label them (check `MemoryBar.svelte` for a `label` prop; add one if needed, defaulting to current behaviour for the memory usage).

4. **Rebuild embedded frontend assets** if the Go binary embeds the built Svelte output (check `html/` build step / `go:embed` directive) so the new UI ships with the binary.

5. **Tests**: add a small unit test for the new `diskUsage()` helper (e.g. assert `total > 0` and `used <= total` and `ok == true` when run against `/` on the test host — this is environment-dependent but `/` always exists on Linux CI runners). Optionally add/extend a `/status` handler test asserting the JSON contains `disk_used`/`disk_total` keys (numeric or null).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/main.go` | Modify | Add `diskUsage()` helper using `unix.Statfs("/", ...)`; wire `disk_used`/`disk_total` into the `/status` JSON response |
| `lazy-tcp-proxy/html/src/App.svelte` | Modify | Read `disk_used`/`disk_total` from the status response; render a second usage bar for disk, labelled distinctly from memory |
| `lazy-tcp-proxy/html/src/lib/MemoryBar.svelte` | Modify (maybe) | Add an optional `label` prop if not already generic enough to be reused for disk |
| `lazy-tcp-proxy/main_test.go` (or wherever `/status` is tested) | Modify/Create | Unit test for `diskUsage()` and/or `/status` JSON shape |

## API Contracts

`GET /status` response gains two new top-level keys, following the exact same nullable-pointer pattern as `memory_used`/`memory_total`:

```json
{
  "services": [...],
  "memory_used": 123456789,
  "memory_total": 987654321,
  "containers": [...],
  "disk_used": 55000000000,
  "disk_total": 250000000000
}
```

- `disk_used` / `disk_total`: `*int64`, bytes. `null` when `statfs("/")` fails (mirrors `memory_used`/`memory_total` nullability — note per REQ-095/current behaviour these serialize as JSON `null`, not omitted keys, and the frontend guards with `?? 0`).
- Values represent the root (`/`) filesystem only — i.e. `df /`, NOT an aggregate of Docker object sizes.
- Independent of backend (Docker or Kubernetes) — same computation either way.

## Data Models

No persistent data model changes. New Go-side struct: none needed — two `*int64` locals suffice, matching the existing `memUsed`/`memTotal` pattern (untyped `map[string]any` JSON encoding).

## Key Code Snippets

```go
// main.go — near the /status handler
func diskUsage() (used, total int64, ok bool) {
    var stat unix.Statfs_t
    if err := unix.Statfs("/", &stat); err != nil {
        return 0, 0, false
    }
    total = int64(stat.Blocks) * stat.Bsize
    used = total - int64(stat.Bavail)*stat.Bsize
    return used, total, true
}
```

```go
// inside the /status handler, alongside memUsed/memTotal setup
var diskUsed, diskTotal *int64
if u, t, ok := diskUsage(); ok {
    diskUsed, diskTotal = &u, &t
}
// ... add to the map[string]any:
// "disk_used":  diskUsed,
// "disk_total": diskTotal,
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestDiskUsage_RootFilesystem` | call `diskUsage()` on a Linux test runner | `ok == true`, `total > 0`, `0 <= used && used <= total` |
| `TestDiskUsage_StatfsError` (optional, if testable via interface seam) | simulate `Statfs` failure | `ok == false`, `used == 0`, `total == 0` |
| `TestStatusHandler_DiskFields` | call `/status` with a fake/real backend | response JSON contains `disk_used` and `disk_total` keys (numeric, since `/` always resolves on CI Linux runners) |

## Risks & Open Questions

- **Container vs. host view**: when the proxy runs inside a container, `statfs("/")` reports the container's root mount — for typical Docker overlay2 setups this reflects the host drive's capacity, but in unusual configurations (e.g. a size-constrained overlay or a separate volume mounted at `/`) it may not equal the true host disk. This matches the requirement's stated scope ("the drive the proxy runs on") and needs no special handling — just worth noting in user-facing docs if it ever causes confusion.
- **`golang.org/x/sys/unix` is Linux/Unix-specific** (`Statfs_t` field types/names can differ slightly across Unix variants, e.g. `Bsize` is `int64` on Linux/amd64 but may be `int32` on other arches — the snippet above assumes Linux, matching the project's existing Linux-only scope; no build tags needed since no darwin/windows code exists in the repo).
- **Frontend label**: confirm whether `MemoryBar.svelte` needs a `label`/`title` prop added, or whether surrounding markup already provides "Memory" / "Disk" section headers — small UI decision to make during Build, not expected to be contentious.
