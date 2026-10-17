# Dashboard Per-Container Memory Column

**Date Added**: 2026-06-06
**Priority**: Low
**Status**: Completed

## Problem Statement

The `/status` endpoint now returns a `containers` array with per-container `memory_used`/`memory_limit` (REQ-109), but the Svelte dashboard's status table does not surface this data — operators can't see at a glance how much memory each managed container is consuming.

## Functional Requirements

1. Add a "memory" column to the status table on the dashboard.
2. For each service row, look up the matching entry in `data.containers` by `container_name` and display its memory usage.
3. When a single container backs more than one service/row, show the memory bar only on the first row for that container (avoid duplicate bars for the same container).
4. Display a progress bar showing memory used out of memory limit, with the formatted used/limit values and percentage shown to the right of the bar.

## User Experience Requirements

- Reuse the same visual style as the existing aggregate memory bar (rounded bar, accent color fill).
- Extracted into a reusable `MemoryBar` component (`html/src/lib/MemoryBar.svelte`) so both the aggregate header bar and the per-row bars share the same rendering/formatting logic.

## Technical Requirements

- New component `MemoryBar.svelte` accepts `used`, `limit`, and an optional `barWidth` class for sizing.
- `App.svelte` derives `serviceRows` (a `$derived.by` over `services`/`containers`) that pairs each service with its container's memory stats and a `showMemory` flag (true only for the first service referencing a given container name).

## Acceptance Criteria

- [x] Status table has a "memory" column showing a progress bar with used/limit bytes and percentage.
- [x] Containers backing multiple services only show the memory bar once (first row).
- [x] Aggregate memory bar at the top of the Status tab uses the same `MemoryBar` component.
- [x] `npm run build` succeeds with no errors.

## Dependencies

- REQ-109: Per-Container Memory Stats in `/status` Endpoint — provides the `containers` array consumed here.
- REQ-103: Svelte + Tailwind HTML Dashboard — the dashboard this change extends.

## Implementation Notes

- Container names from `/status` are matched against `snap.container_name` on each service entry.
- `MemoryBar` formats bytes (B/KB/MB/GB) and computes percentage internally; `barWidth` defaults to `w-20` for table rows and is overridden to `w-64` for the larger aggregate bar.
