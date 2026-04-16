# Webhook Usage Metrics — Implementation Plan

**Requirement**: [2026-04-16-webhook-usage-metrics.md](2026-04-16-webhook-usage-metrics.md)
**Date**: 2026-04-16
**Status**: Implemented

## Implementation Steps

1. **Add `listenPort int` to `targetState`** — store the TCP listen port on the struct
   so `handleConn` can read it without needing a map lookup.

2. **Add `listenPort int` to `udpListenerState`** — same reason for UDP.

3. **Add new fields to `webhookPayload`** — extend the struct with seven new
   `omitempty` JSON fields; `fireWebhook` gains no new parameters (struct is passed
   directly instead).

4. **Refactor `fireWebhook` to accept `webhookPayload` directly** — removes the
   long parameter list; callers build the struct, `fireWebhook` only sets `Timestamp`
   and truncates `ContainerID` to 12 chars. Update all eight call sites.

5. **Add `countingWriter`** — a thin `io.Writer` wrapper that accumulates bytes
   copied; used in place of bare `io.CopyBuffer` for the two TCP proxy goroutines.

6. **Update `handleConn`** — capture `startedAt`, plumb `listenPort`, wrap both
   `io.CopyBuffer` calls with counting writers, capture `upstreamAddr` after dial,
   and pass all new fields on both `tcp_conn_start` and `tcp_conn_end`.

7. **Add metrics fields to `udpFlow`** — `startedAt time.Time`, `bytesSent int64`,
   `bytesReceived int64`, `upstreamAddr string`.

8. **Update `startUDPFlow`** — populate the new `udpFlow` fields at flow creation,
   pass `listenPort` and `startedAt` on `udp_flow_start` (both leader and non-leader
   paths).

9. **Update `udpReadLoop`** — after each successful upstream write, atomically
   increment `flow.bytesSent`.

10. **Update `udpUpstreamReadLoop`** — after each successful upstream read, atomically
    increment `flow.bytesReceived`.

11. **Update `udpFlowSweeper`** — pass all new fields on `udp_flow_end`.

12. **Update `README.md`** — revise the Webhooks section: update the event table,
    add field descriptions, and replace the two example JSON blocks with four
    (start + end for both TCP and UDP).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | Steps 1, 3, 4, 5, 6 |
| `lazy-tcp-proxy/internal/proxy/udp.go` | Modify | Steps 2, 7, 8, 9, 10, 11 |
| `README.md` | Modify | Step 12 |
| `requirements/2026-04-16-webhook-usage-metrics.md` | Modify | Status → In Progress → Completed |
| `requirements/_index.md` | Modify | Status → In Progress → Completed |

## API Contracts

### `webhookPayload` struct (after change)

```go
type webhookPayload struct {
    // Existing fields — unchanged
    Event         string `json:"event"`
    ConnectionID  string `json:"connection_id,omitempty"`
    RemoteAddr    string `json:"remote_addr,omitempty"`
    RemotePort    int    `json:"remote_port,omitempty"`
    ContainerID   string `json:"container_id"`
    ContainerName string `json:"container_name"`
    Timestamp     string `json:"timestamp"`

    // New fields
    ListenPort    int    `json:"listen_port,omitempty"`
    UpstreamAddr  string `json:"upstream_addr,omitempty"`
    StartedAt     string `json:"started_at,omitempty"`
    EndedAt       string `json:"ended_at,omitempty"`
    DurationMs    int64  `json:"duration_ms,omitempty"`
    BytesSent     int64  `json:"bytes_sent,omitempty"`
    BytesReceived int64  `json:"bytes_received,omitempty"`
}
```

`omitempty` behaviour: fields are absent from the JSON output when zero-valued.
Container lifecycle events (`container_started`, `container_stopped`) naturally
produce no new fields because they are zero-valued. Start events include `listen_port`
and `started_at` (non-zero); end events include all seven new fields.

### `fireWebhook` new signature

```go
func (s *ProxyServer) fireWebhook(webhookURL string, payload webhookPayload)
```

`fireWebhook` sets `payload.Timestamp = time.Now().UTC().Format(time.RFC3339)` and
truncates `payload.ContainerID` to 12 characters before marshalling. Callers do not
set `Timestamp`.

## Key Code Snippets

### countingWriter (server.go)

```go
// countingWriter wraps an io.Writer and counts bytes written.
type countingWriter struct {
    w io.Writer
    n *int64
}

func (cw countingWriter) Write(p []byte) (int, error) {
    n, err := cw.w.Write(p)
    *cw.n += int64(n)
    return n, err
}
```

No atomics needed for TCP: each goroutine writes only to its own counter
(`bytesSent` or `bytesReceived`), and `wg.Wait()` provides the happens-before
guarantee before the defer reads them.

### handleConn — start event and counters

```go
startedAt := time.Now()
if ts.info.WebhookURL != "" {
    go s.fireWebhook(ts.info.WebhookURL, webhookPayload{
        Event:         "tcp_conn_start",
        ConnectionID:  connID,
        RemoteAddr:    remoteIP,
        RemotePort:    remotePort,
        ContainerID:   ts.info.ContainerID,
        ContainerName: ts.info.ContainerName,
        ListenPort:    ts.listenPort,
        StartedAt:     startedAt.UTC().Format(time.RFC3339),
    })
    defer func() {
        endedAt := time.Now()
        go s.fireWebhook(ts.info.WebhookURL, webhookPayload{
            Event:         "tcp_conn_end",
            ConnectionID:  connID,
            RemoteAddr:    remoteIP,
            RemotePort:    remotePort,
            ContainerID:   ts.info.ContainerID,
            ContainerName: ts.info.ContainerName,
            ListenPort:    ts.listenPort,
            UpstreamAddr:  upstreamAddr,
            StartedAt:     startedAt.UTC().Format(time.RFC3339),
            EndedAt:       endedAt.UTC().Format(time.RFC3339),
            DurationMs:    endedAt.Sub(startedAt).Milliseconds(),
            BytesSent:     bytesSent,
            BytesReceived: bytesReceived,
        })
    }()
}
```

`upstreamAddr` is declared as `var upstreamAddr string` before the defer, then set
after the dial: `upstreamAddr = upstream.RemoteAddr().String()`. Because the defer
captures `upstreamAddr` by reference (via a closure over the variable), it reads the
final value at the time the defer executes.

`bytesSent` and `bytesReceived` are declared as `var bytesSent, bytesReceived int64`
before the copy goroutines, and are safe to read in the defer because `wg.Wait()`
completes before `handleConn` returns.

### TCP copy goroutines with counting

```go
var bytesSent, bytesReceived int64

go func() {
    defer wg.Done()
    buf := copyBufPool.Get().(*[]byte)
    defer copyBufPool.Put(buf)
    io.CopyBuffer(countingWriter{upstream, &bytesSent}, conn, *buf) //nolint:errcheck
    closeAll()
}()

go func() {
    defer wg.Done()
    buf := copyBufPool.Get().(*[]byte)
    defer copyBufPool.Put(buf)
    io.CopyBuffer(countingWriter{conn, &bytesReceived}, upstream, *buf) //nolint:errcheck
    closeAll()
}()
```

### udpFlow — new fields

```go
type udpFlow struct {
    clientAddr   *net.UDPAddr
    upstreamConn *net.UDPConn
    lastActive   time.Time
    connectionID string
    startedAt    time.Time // NEW
    upstreamAddr string    // NEW
    bytesSent    int64     // NEW — protected by atomic ops (multiple writers possible)
    bytesReceived int64    // NEW — protected by atomic ops
}
```

### udpReadLoop — byte counting

Replace the existing write call:
```go
if _, err := conn.Write(data); err != nil {
    log.Printf("proxy: udp: write to upstream for \033[33m%s\033[0m failed: %v", uls.info.ContainerName, err)
}
```
With:
```go
n, err := conn.Write(data)
atomic.AddInt64(&flow.bytesSent, int64(n))
if err != nil {
    log.Printf("proxy: udp: write to upstream for \033[33m%s\033[0m failed: %v", uls.info.ContainerName, err)
}
```

### udpUpstreamReadLoop — byte counting

After the `WriteToUDP` call:
```go
atomic.AddInt64(&flow.bytesReceived, int64(n))
```

### udpFlowSweeper — end event

```go
endedAt := now // captured at sweep time, used per-flow
if uls.info.WebhookURL != "" {
    connID := flow.connectionID
    clientAddr := flow.clientAddr
    fa := flow.upstreamAddr
    sa := flow.startedAt
    bs := atomic.LoadInt64(&flow.bytesSent)
    br := atomic.LoadInt64(&flow.bytesReceived)
    go s.fireWebhook(uls.info.WebhookURL, webhookPayload{
        Event:         "udp_flow_end",
        ConnectionID:  connID,
        RemoteAddr:    clientAddr.IP.String(),
        RemotePort:    clientAddr.Port,
        ContainerID:   uls.info.ContainerID,
        ContainerName: uls.info.ContainerName,
        ListenPort:    uls.listenPort,
        UpstreamAddr:  fa,
        StartedAt:     sa.UTC().Format(time.RFC3339),
        EndedAt:       endedAt.UTC().Format(time.RFC3339),
        DurationMs:    endedAt.Sub(sa).Milliseconds(),
        BytesSent:     bs,
        BytesReceived: br,
    })
}
```

Local variables are captured before the goroutine launch because `flow` is deleted
from the map immediately after (still holding `uls.mu`).

## Unit Tests

No new unit-test files are planned. The existing integration tests exercise the TCP
and UDP proxy paths end-to-end; if the proxy still compiles and the tests pass, the
byte-counting wrappers are confirmed correct. A manual verification with a known
payload size (e.g. echo server) can confirm values if desired.

| Test | Input | Expected Output |
|------|-------|-----------------|
| Existing TCP integration test | N bytes sent | `tcp_conn_end` includes `bytes_sent ≥ N` |
| Existing UDP integration test | N bytes sent | `udp_flow_end` includes `bytes_sent ≥ N` |
| Container lifecycle webhook | container_started event | No new fields in payload |

## Risks & Open Questions

- **`omitempty` on zero `int64`**: `bytes_sent: 0` and `duration_ms: 0` will be omitted
  from the JSON for zero-byte or sub-millisecond connections. Accepted trade-off.
- **UDP first-datagram bytes**: The leader goroutine sends `firstDatagram` before the
  `udpFlow` struct is created. The leader path initialises `bytesSent` with
  `int64(len(firstDatagram))` at flow creation time; the non-leader path does the same.
  The initial response datagram from the upstream (forwarded to the client in the leader
  readiness probe) is counted as `bytesReceived` by the normal `udpUpstreamReadLoop`.
  Actually, the first response is forwarded by the leader probe directly, bypassing
  `udpUpstreamReadLoop`. Therefore the leader should also seed `bytesReceived` with the
  size of the first response datagram `n` captured in the readiness loop.
