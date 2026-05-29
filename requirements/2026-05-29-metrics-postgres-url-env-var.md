# Metrics PostgreSQL URL Environment Variable

**Date Added**: 2026-05-29
**Priority**: Medium
**Status**: Completed

## Problem Statement

There is no mechanism to enable or disable metrics gathering at runtime. A PostgreSQL connection URL needs to be provided via environment variable so operators can opt in to metrics collection without rebuilding the image. When the variable is absent, the proxy must continue to operate normally with no metrics overhead.

## Functional Requirements

1. Introduce a new environment variable `METRICS_POSTGRES_URL` that accepts a standard PostgreSQL connection URL (e.g. `postgres://user:password@host:5432/dbname`).
2. On startup, if `METRICS_POSTGRES_URL` is set and non-empty:
   - Log a startup message confirming metrics are enabled, including the host/database but **masking credentials**.
   - Enable the metrics subsystem.
3. On startup, if `METRICS_POSTGRES_URL` is absent or empty:
   - Log a startup message stating metrics are disabled.
   - Skip all metrics initialisation — zero overhead to normal proxy operation.
4. The URL value is never printed in full (credentials must be redacted in all log output).

## User Experience Requirements

- No change to existing behaviour when `METRICS_POSTGRES_URL` is not set.
- Operators can enable metrics by adding a single environment variable — no config file changes required.

## Technical Requirements

- Parse and validate the URL at startup using Go's `net/url` package; log a clear error and disable metrics if the URL is malformed.
- Metrics on/off state is a single boolean (or nil pointer to a metrics struct) checked before any metrics write — no locking overhead on the hot path.
- Follow the existing env-var pattern in `main.go` (read via `os.Getenv`, log via the structured logger).

### Rollup schedule

- A dedicated goroutine fires a `time.NewTicker(1 * time.Minute)` rollup loop.
- On each tick, connection counters are read via atomic operations (lock-free, non-blocking on the hot path).
- The PostgreSQL write is dispatched as a fire-and-forget goroutine so the ticker is never delayed by a slow write.
- Each write uses `context.WithTimeout` of **15 seconds**.

### Write failure handling

- Failed snapshots are held in an in-memory ring buffer capped at **5 entries** (oldest dropped when the buffer is full).
- Each snapshot retains its original `rollup_at` timestamp.
- On the next tick, the retry buffer is flushed first: each buffered snapshot is written as a **separate database record** (preserving the original timestamp), followed by the current snapshot. This ensures graphs remain correctly time-stamped and are never skewed by aggregation.
- On successful flush of a buffered snapshot, it is discarded from the buffer.
- On failure, a `WARN` log line is emitted: `metrics write failed (buffered N/5): <error>`.

## Acceptance Criteria

- [ ] `METRICS_POSTGRES_URL` unset → startup log line: `metrics disabled (METRICS_POSTGRES_URL not set)`
- [ ] `METRICS_POSTGRES_URL` set to a valid URL → startup log line: `metrics enabled (host=<host> db=<dbname>)` with no credentials visible
- [ ] `METRICS_POSTGRES_URL` set to a malformed value → startup log line: `metrics disabled (invalid METRICS_POSTGRES_URL: <error>)` and proxy starts normally
- [ ] No functional regression in proxy behaviour when metrics are disabled
- [ ] Credentials never appear in any log output
- [ ] Rollup ticker fires every 1 minute and does not block the proxy hot path
- [ ] DB write timeout is 15 seconds
- [ ] Up to 5 failed snapshots are retained in the retry buffer; the 6th pushes out the oldest
- [ ] On recovery, each buffered snapshot is written as a separate record with its original `rollup_at` timestamp
- [ ] Write failure emits a WARN log: `metrics write failed (buffered N/5): <error>`

## Dependencies

- Lays the foundation for REQ-090 (metrics schema, rollup loop, and PostgreSQL writes) — that requirement will implement the actual data collection; this requirement only wires the env var and toggle.

## Implementation Notes

- The metrics subsystem struct (even if empty at this stage) should live in a new package `internal/metrics` to keep future growth isolated from `main.go`.
- Credential masking: replace userinfo in the parsed URL with `***:***` before logging.
