# /metrics Heatmap — Third State for Active Connections

**Date Added**: 2026-06-06
**Priority**: Low
**Status**: Completed

## Problem Statement

The `/metrics` hourly-activity heatmap currently distinguishes only two states per hour: `0` (not active — no uptime) and `1` (active — the upstream container had uptime during that hour). There is no way to tell, from the heatmap data alone, whether an hour with uptime also had real client traffic (active connections), versus the container simply being kept warm/running with no usage.

## Functional Requirements

- The hourly heatmap cells in the `/metrics` response may now contain the value `2`, in addition to `0` and `1`:
  - `0` = not active (no uptime recorded for that hour)
  - `1` = active (uptime recorded, but no connections were started)
  - `2` = active with connections (uptime recorded AND at least one connection was started during that hour)
- An hour is marked `2` when `SUM(connections_started) > 0` for the rolled-up rows in that hour (in addition to the existing `uptime_ms_total > 0` filter).

## User Experience Requirements

- On the dashboard heatmap, cells with value `2` are coloured green to visually distinguish "active with real traffic" from "active but idle" (`1`, orange) and "not active" (`0`, dark background).

## Technical Requirements

- Update `queryHourlyActivity` in `internal/metrics/postgres.go` to aggregate `SUM(connections_started)` per (container_name, port, is_udp, dow, hr) group.
- Update `hourlyActivity()` to set the cell to `2` when the summed connection count is greater than zero, otherwise `1`.
- Update the Svelte dashboard heatmap cell rendering in `html/src/App.svelte` to map cell value `2` to a green Tailwind background class.

## Acceptance Criteria

- [x] Hours with uptime but zero `connections_started` render as value `1` (orange).
- [x] Hours with uptime and at least one `connections_started` render as value `2` (green).
- [x] Hours with no uptime remain value `0` (dark background).

## Dependencies

- REQ-107 (`/metrics` Weekly Heatmap Response Shape)
- REQ-108 (Dashboard Tab Navigation: Status & Metrics Heatmap)
