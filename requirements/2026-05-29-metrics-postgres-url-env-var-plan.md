# Metrics PostgreSQL URL Environment Variable — Implementation Plan

**Requirement**: [2026-05-29-metrics-postgres-url-env-var.md](2026-05-29-metrics-postgres-url-env-var.md)
**Date**: 2026-05-29
**Status**: Draft

## Implementation Steps

1. Update requirement status to "In Progress" in requirement file and index ✅
2. Add `github.com/jackc/pgx/v5` dependency via `go get`
3. Create `internal/metrics/collector.go` — `Snapshot` struct, `portAccumulator`, `Collector`, `Run()` rollup loop
4. Create `internal/metrics/postgres.go` — DB connect, `CREATE TABLE IF NOT EXISTS`, `INSERT`, retry buffer
5. Add `resolveMetricsPostgresURL()` to `main.go`
6. Add `collector *metrics.Collector` field and `SetCollector()` to `ProxyServer`; add `RegisterPort` / `UnregisterPort` calls in `RegisterTarget` / `RemoveTarget`
7. Add `countingWriter` to `internal/proxy/server.go`
8. Wire metric calls into `handleConn`: `ConnectionStarted`, cold-start timing, `ContainerColdStart`, `ConnectionEnded` (with bytes + duration), `ConnectionFailed`
9. Wire metric calls into `handleHTTPProxy`: byte counting, `ConnectionEnded`
10. Wire `ContainerStarted` / `ContainerStopped` uptime hooks into `ProxyServer.ContainerStarted` / `ContainerStopped`
11. Wire UDP byte counting and flow metrics into `internal/proxy/udp.go`
12. Wire collector init, `srv.SetCollector`, and `go collector.Run(ctx)` into `main.go`
13. Run `go test ./...`; verify no regressions
14. Update requirement status to "Completed"

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/metrics/collector.go` | Create | Per-port accumulators, Collector, rollup logic |
| `internal/metrics/postgres.go` | Create | pgxpool, schema, INSERT, retry buffer |
| `main.go` | Modify | `resolveMetricsPostgresURL()`, collector init, `SetCollector`, `go collector.Run(ctx)` |
| `internal/proxy/server.go` | Modify | `collector` field, `SetCollector`, `RegisterPort`/`UnregisterPort`, `countingWriter`, metric calls in `handleConn`, `handleHTTPProxy`, `ContainerStarted`, `ContainerStopped`, `RegisterTarget`, `RemoveTarget` |
| `internal/proxy/udp.go` | Modify | Byte counting, flow metric calls |
| `go.mod` / `go.sum` | Modify | Add `github.com/jackc/pgx/v5` |

## Data Model

```sql
CREATE TABLE IF NOT EXISTS proxy_metrics (
    id                      BIGSERIAL PRIMARY KEY,
    rollup_at               TIMESTAMPTZ NOT NULL,
    container_name          TEXT NOT NULL,
    port                    INTEGER NOT NULL,
    is_udp                  BOOLEAN NOT NULL DEFAULT FALSE,
    availability            TEXT NOT NULL,
    connections_started     BIGINT NOT NULL DEFAULT 0,
    connections_ended       BIGINT NOT NULL DEFAULT 0,
    connections_active      INTEGER NOT NULL DEFAULT 0,
    connections_peak        INTEGER NOT NULL DEFAULT 0,
    connections_failed      BIGINT NOT NULL DEFAULT 0,
    bytes_sent              BIGINT NOT NULL DEFAULT 0,
    bytes_received          BIGINT NOT NULL DEFAULT 0,
    request_duration_ms_avg DOUBLE PRECISION,
    request_duration_ms_max BIGINT,
    request_duration_ms_min BIGINT,
    container_starts        BIGINT NOT NULL DEFAULT 0,
    cold_start_ms_avg       DOUBLE PRECISION,
    cold_start_ms_max       BIGINT,
    container_stops         BIGINT NOT NULL DEFAULT 0,
    uptime_ms_total         BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS proxy_metrics_rollup_at ON proxy_metrics (rollup_at);
CREATE INDEX IF NOT EXISTS proxy_metrics_container  ON proxy_metrics (container_name, port, rollup_at);
```

`request_duration_ms_avg/max/min` and `cold_start_ms_avg/max` are `NULL` when no samples exist in the window.

## Key Code Snippets

### resolveMetricsPostgresURL — main.go

```go
func resolveMetricsPostgresURL() string {
    raw := os.Getenv("METRICS_POSTGRES_URL")
    if raw == "" {
        log.Println("metrics: disabled (METRICS_POSTGRES_URL not set)")
        return ""
    }
    u, err := url.Parse(raw)
    if err != nil {
        log.Printf("metrics: disabled (invalid METRICS_POSTGRES_URL: %v)", err)
        return ""
    }
    safe := *u
    if safe.User != nil {
        safe.User = url.UserPassword("***", "***")
    }
    log.Printf("metrics: enabled (host=%s db=%s)", u.Hostname(), strings.TrimPrefix(u.Path, "/"))
    return raw
}
```

### portAccumulator — internal/metrics/collector.go

```go
type portAccumulator struct {
    // static metadata (written once at register time)
    containerName string
    port          int
    isUDP         bool
    availability  string

    // atomic counters — incremented on the hot path, Swap(0) at rollup
    connectionsStarted  atomic.Int64
    connectionsEnded    atomic.Int64
    connectionsFailed   atomic.Int64
    bytesSent           atomic.Int64
    bytesReceived       atomic.Int64
    containerStarts     atomic.Int64
    containerStops      atomic.Int64

    // active conns — incremented/decremented but NOT reset at rollup (it's a gauge)
    activeConns atomic.Int32
    // peak — CAS-updated when activeConns increases; Swap(0) at rollup
    peakConns   atomic.Int32

    // duration and cold-start stats need mutex because avg requires count+total together
    mu               sync.Mutex
    durationCount    int64
    durationTotalMs  int64
    durationMaxMs    int64
    durationMinMs    int64 // math.MaxInt64 when no samples

    coldStartCount   int64
    coldStartTotalMs int64
    coldStartMaxMs   int64

    // uptime state (also mu-protected)
    containerRunning bool
    containerUpAt    time.Time
    uptimeAccumMs    int64

    windowStart time.Time // start of current window
}
```

### rollup — gap-free window boundary

At each tick `windowEnd = time.Now()` is captured once and passed to every accumulator:

```go
func (a *portAccumulator) rollup(windowEnd time.Time) Snapshot {
    started := a.connectionsStarted.Swap(0)
    ended   := a.connectionsEnded.Swap(0)
    failed  := a.connectionsFailed.Swap(0)
    sent    := a.bytesSent.Swap(0)
    recv    := a.bytesReceived.Swap(0)
    cStart  := a.containerStarts.Swap(0)
    cStop   := a.containerStops.Swap(0)
    active  := a.activeConns.Load()   // gauge: not reset
    peak    := a.peakConns.Swap(0)

    a.mu.Lock()

    // uptime: add time-to-windowEnd if container is currently running,
    // then reset accumulator and move the start boundary to windowEnd.
    uptime := a.uptimeAccumMs
    if a.containerRunning {
        uptime += windowEnd.Sub(a.containerUpAt).Milliseconds()
        a.containerUpAt = windowEnd // next window starts here — no gap
    }
    a.uptimeAccumMs = 0

    // duration stats
    snap := Snapshot{
        RollupAt:           a.windowStart,
        ContainerName:      a.containerName,
        Port:               a.port,
        IsUDP:              a.isUDP,
        Availability:       a.availability,
        ConnectionsStarted: started,
        ConnectionsEnded:   ended,
        ConnectionsFailed:  failed,
        ConnectionsActive:  active,
        ConnectionsPeak:    peak,
        BytesSent:          sent,
        BytesReceived:      recv,
        ContainerStarts:    cStart,
        ContainerStops:     cStop,
        UptimeMsTotal:      uptime,
    }
    if a.durationCount > 0 {
        avg := float64(a.durationTotalMs) / float64(a.durationCount)
        snap.RequestDurationMsAvg = &avg
        snap.RequestDurationMsMax = &a.durationMaxMs
        snap.RequestDurationMsMin = &a.durationMinMs
    }
    a.durationCount, a.durationTotalMs, a.durationMaxMs, a.durationMinMs = 0, 0, 0, math.MaxInt64

    if a.coldStartCount > 0 {
        avg := float64(a.coldStartTotalMs) / float64(a.coldStartCount)
        snap.ColdStartMsAvg = &avg
        snap.ColdStartMsMax = &a.coldStartMaxMs
    }
    a.coldStartCount, a.coldStartTotalMs, a.coldStartMaxMs = 0, 0, 0

    a.windowStart = windowEnd // advance window — contiguous, no gap
    a.mu.Unlock()

    return snap
}
```

### Retry buffer — internal/metrics/postgres.go

```go
type retryBuffer struct {
    mu  sync.Mutex
    buf []*Snapshot // len ≤ 5, oldest first
}

func (rb *retryBuffer) push(s *Snapshot) {
    rb.mu.Lock()
    defer rb.mu.Unlock()
    if len(rb.buf) >= 5 {
        rb.buf = rb.buf[1:] // drop oldest
    }
    rb.buf = append(rb.buf, s)
}

func (rb *retryBuffer) drain() []*Snapshot {
    rb.mu.Lock()
    defer rb.mu.Unlock()
    out := rb.buf
    rb.buf = nil
    return out
}
```

On each tick the write goroutine:
1. Drains the buffer (oldest first) and attempts each write with a 15 s timeout.
2. On failure: logs `metrics write failed (buffered N/5): <err>`, pushes the snapshot back.
3. Writes the current snapshot; on failure pushes it to the buffer.

### Run loop — internal/metrics/collector.go

```go
func (c *Collector) Run(ctx context.Context) {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            windowEnd := time.Now()
            snaps := c.rollupAll(windowEnd)
            go c.flush(ctx, snaps) // fire-and-forget; never delays next tick
        }
    }
}
```

### countingWriter — internal/proxy/server.go

```go
type countingWriter struct {
    w io.Writer
    n atomic.Int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
    written, err := cw.w.Write(p)
    cw.n.Add(int64(written))
    return written, err
}
```

Used in `handleConn`:

```go
connStart := time.Now()
// ... existing setup ...

var sentCW, recvCW countingWriter
sentCW.w, recvCW.w = upstream, conn

// existing goroutines changed to write through counters:
io.CopyBuffer(&sentCW, conn, *buf)     // client→upstream  = bytes_sent
io.CopyBuffer(&recvCW, upstream, *buf) // upstream→client  = bytes_received

wg.Wait()
durationMs := time.Since(connStart).Milliseconds()
if s.collector != nil {
    s.collector.ConnectionEnded(ts.targetPort, false, durationMs,
        sentCW.n.Load(), recvCW.n.Load())
}
```

### Cold-start detection — handleConn

```go
coldStart := time.Now()
_, startErr, shared := s.startGroup.Do(ts.info.ContainerID, func() (any, error) {
    return nil, s.backend.EnsureRunning(ctx, ts.info.ContainerID)
})
if startErr != nil {
    if s.collector != nil {
        s.collector.ConnectionFailed(ts.targetPort, false)
    }
    // ... existing log + return ...
}
if !shared && s.collector != nil {
    // only the initiating goroutine records a container start
    s.collector.ContainerColdStart(ts.targetPort, false, time.Since(coldStart).Milliseconds())
}
```

### Uptime hooks — ProxyServer.ContainerStarted / ContainerStopped

```go
func (s *ProxyServer) ContainerStarted(containerID string) {
    // ... existing lock + set running=true ...
    if s.collector != nil {
        for _, ts := range s.targets {
            if ts.info.ContainerID == containerID {
                s.collector.OnContainerRunning(ts.targetPort, false, time.Now())
            }
        }
        // same for udpTargets
    }
}
```

## Unit Tests

| Test | Input | Expected |
|------|-------|----------|
| `METRICS_POSTGRES_URL` unset | env not set | log "disabled", nil returned |
| `METRICS_POSTGRES_URL` malformed | `"not a url%%"` | log "disabled (invalid…)", nil returned |
| `METRICS_POSTGRES_URL` valid | `"postgres://u:p@host/db"` | log "enabled (host=host db=db)", credentials not in log |
| rollup gap-free | container up at t=0:30, rollup at t=1:00, t=2:00 | window 1: uptime=30000ms; window 2: uptime=60000ms |
| retry buffer push 6 | push 6 snapshots | oldest (snap 1) dropped; buf contains snaps 2–6 |
| retry buffer drain | drain after 3 pushes | returns [snap1, snap2, snap3] oldest-first; buf empty |
| peak conns | 3 conns open, 1 closes, rollup | peak=3, active=2 |

## Risks & Open Questions

- **UDP byte counting**: UDP flows use a different copy path in `udp.go` — needs the same `countingWriter` approach but must be confirmed against the actual UDP loop structure.
- **pgx version**: `pgx/v5` is pure Go and already compatible with Go 1.24; no CGO required.
- **Schema migration**: `CREATE TABLE IF NOT EXISTS` is idempotent on first run. No migration tool needed at this stage; a future requirement can add one if the schema evolves.
- **`handleHTTPProxy` bytes**: requests/responses are written via `req.Write` / `resp.Write` to wrapped connections — the `countingWriter` wrapper must be applied to `client` and `upstream` before they are passed in.
