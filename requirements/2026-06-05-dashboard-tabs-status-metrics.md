# Dashboard Tab Navigation: Status & Metrics Heatmap

**Date Added**: 2026-06-05
**Priority**: Medium
**Status**: Completed

## Problem Statement

The dashboard is a single flat view. The `/metrics` endpoint (REQ-107) now returns weekly hourly
activity data per service, but there is nowhere to surface it. Users have no way to see historical
usage patterns.

## Functional Requirements

1. The page header gains a tab bar: **Status** (default) and **Metrics**.
2. **Status tab** — existing table + memory bar, unchanged.
3. **Metrics tab** — fetches `GET /metrics` on first visit and renders one heatmap block per
   service entry.
   - Each heatmap shows: service label (`container_name:port[/udp]`), 7 rows (Mon–Sun), 24 columns
     (hours 0–23).
   - Active cells (`1`) use a warm accent colour; inactive (`0`) cells use a muted background.
   - Row labels on the left; hour tick labels (0, 6, 12, 18, 23) below the grid.
   - Services with `active: false` are still shown but dimmed.

## User Experience Requirements

- Active tab has a visible underline/filled indicator; inactive tab text is muted.
- Switching tabs is instant; Metrics data is lazily fetched on first switch.
- Style: same stone dark palette (`#1C1917`, `#292524`, `#3B3837`, `#D97757`, `#78716C`, `#FAFAF9`)
  as the rest of the UI.

## Technical Requirements

- Tab state managed with Svelte `$state` in `App.svelte`.
- Metrics data fetched from `/metrics` (dev: `http://localhost:8080/metrics`).
- No new npm dependencies.
- All changes confined to `html/src/App.svelte`.

## Acceptance Criteria

- [ ] Tab bar renders with "Status" active by default.
- [ ] Status tab shows existing table and memory bar unchanged.
- [ ] Metrics tab fetches `/metrics` on first visit and renders a heatmap per service.
- [ ] Heatmap cells correctly reflect 0/1 values from the API.
- [ ] Layout is readable at 1280px width with no horizontal overflow.

## Dependencies

- REQ-103 (Svelte + Tailwind HTML Dashboard)
- REQ-107 (/metrics Weekly Heatmap Response Shape)
