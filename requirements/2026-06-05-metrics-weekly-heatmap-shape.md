# /metrics Weekly Heatmap Response Shape

**Date Added**: 2026-06-05
**Priority**: Medium
**Status**: Completed

## Problem Statement

The initial `/metrics` endpoint (REQ-105) returned a flat list of `(container_name, port, is_udp, hour, active)` rows. This shape requires the consumer to do significant grouping and pivoting before rendering a weekly heatmap. The endpoint should instead return one entry per service with pre-aggregated day-of-week arrays so the UI can render directly.

## Functional Requirements

1. `GET /metrics` returns a JSON array with one object per `(container_name, port, is_udp)` tuple.
2. Each object contains seven 24-element integer arrays, one per day of the week: `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`.
3. Each array index corresponds to the hour of the day (0–23). A value of `1` means at least one `proxy_metrics` row in the last 7 days for that weekday+hour had `uptime_ms_total > 0`; `0` means no such row.
4. Day-of-week buckets are fixed (Sunday = `sun`, Monday = `mon`, … Saturday = `sat`) and accumulate **any matching weekday within the 7-day window** — i.e. the second interpretation: if the last 7 days contain two Mondays, both contribute to `mon`.
5. `active: true` is included on each entry if any cell across all seven arrays is `1`.

## Acceptance Criteria

- [ ] Response is a JSON array; each element has `container_name`, `port`, `is_udp`, `mon`–`sun` (each a 24-element array of 0/1), and `active`.
- [ ] An hour with no `uptime_ms_total > 0` rows emits `0`; any matching row emits `1`.
- [ ] `active` is `true` iff at least one cell is `1`.
- [ ] Empty result (no data) returns `[]`.
- [ ] Lint and tests pass.

## Dependencies

Supersedes the flat row shape defined in REQ-105.
