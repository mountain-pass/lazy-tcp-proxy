# Fix UDP Cold-Start Timeouts — Implementation Plan

**Requirement**: [2026-04-14-fix-udp-pihole-cold-start-timeouts.md](2026-04-14-fix-udp-pihole-cold-start-timeouts.md)
**Date**: 2026-04-14
**Status**: Implemented

## Implementation Steps

1. **`types.go`** — Add `StartTimeout *time.Duration` to `TargetInfo`; add `ParseStartTimeoutLabel`
2. **`types_test.go`** — Add unit tests for `ParseStartTimeoutLabel` (mirrors `ParseIdleTimeoutLabel` tests)
3. **`docker/manager.go`** — Parse `lazy-tcp-proxy.start-timeout-secs` label in `containerToTargetInfo`
4. **`k8s/backend.go`** — Parse `lazy-tcp-proxy.start-timeout-secs` annotation in `deploymentToTargetInfo`
5. **`main.go`** — Add `defaultStartTimeout`, `resolveStartTimeout()`, log it, pass to `NewServer`
6. **`proxy/server.go`** — Add `startTimeout` to `ProxyServer`; update `NewServer`; populate field in `RegisterTarget` (new and update paths); reset `upstreamReady` in `ContainerStopped`
7. **`proxy/udp.go`** — Replace fixed retry constants with `startTimeout`-derived retries; add readiness fields to `udpListenerState`; rewrite `startUDPFlow` with leader/follower/fast-path logic

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | Add `StartTimeout *time.Duration` to `TargetInfo`; add `ParseStartTimeoutLabel` |
| `internal/types/types_test.go` | Modify | Add tests for `ParseStartTimeoutLabel` |
| `internal/docker/manager.go` | Modify | Parse `lazy-tcp-proxy.start-timeout-secs` label |
| `internal/k8s/backend.go` | Modify | Parse `lazy-tcp-proxy.start-timeout-secs` annotation |
| `main.go` | Modify | `resolveStartTimeout()`, log, pass to `NewServer` |
| `internal/proxy/server.go` | Modify | `startTimeout` field, updated `NewServer`, `RegisterTarget`, `ContainerStopped` |
| `internal/proxy/udp.go` | Modify | Leader/follower readiness pattern; `uls.lastActive` fix; stop-on-timeout |

## Key Code Snippets

### `types.go` additions

```go
// In TargetInfo struct:
StartTimeout *time.Duration // nil = use global default; non-nil = per-container override

// New function (mirrors ParseIdleTimeoutLabel):
func ParseStartTimeoutLabel(name, raw string) *time.Duration {
    v := strings.TrimSpace(raw)
    if v == "" {
        return nil
    }
    n, err := strconv.Atoi(v)
    if err != nil || n < 0 {
        log.Printf("container %s: ignoring invalid start-timeout-secs %q", name, raw)
        return nil
    }
    d := time.Duration(n) * time.Second
    return &d
}
```

### `main.go` additions

```go
const defaultStartTimeout = 30 * time.Second

func resolveStartTimeout() time.Duration {
    raw := os.Getenv("START_TIMEOUT_SECS")
    if raw == "" {
        return defaultStartTimeout
    }
    n, err := strconv.Atoi(raw)
    if err != nil || n < 0 {
        log.Printf("START_TIMEOUT_SECS=%q is invalid; using default %s", raw, defaultStartTimeout)
        return defaultStartTimeout
    }
    return time.Duration(n) * time.Second
}
```

`NewServer` gains a `startTimeout time.Duration` parameter (appended after existing params).

### `proxy/server.go` — `udpListenerState` creation in `RegisterTarget`

New path:
```go
uls := &udpListenerState{
    listenConn:   pc.(*net.UDPConn),
    targetPort:   m.TargetPort,
    info:         info,
    idleTimeout:  info.IdleTimeout,
    startTimeout: effectiveTimeout(info.StartTimeout, s.startTimeout),
    running:      info.Running,
    flows:        make(map[string]*udpFlow),
    pending:      make(map[string]bool),
}
uls.upstreamReadyCond = sync.NewCond(&uls.mu)
```

Update path (existing target): add `existing.startTimeout = effectiveTimeout(info.StartTimeout, s.startTimeout)`.

### `proxy/server.go` — `ContainerStopped` reset

```go
func (s *ProxyServer) ContainerStopped(containerID string) {
    s.mu.RLock()
    var info types.TargetInfo
    var affectedULS []*udpListenerState
    for _, ts := range s.targets {
        if ts.info.ContainerID == containerID {
            ts.running = false
            info = ts.info
        }
    }
    for _, uls := range s.udpTargets {
        if uls.info.ContainerID == containerID {
            uls.running = false
            info = uls.info
            affectedULS = append(affectedULS, uls)
        }
    }
    s.mu.RUnlock()
    for _, uls := range affectedULS {
        uls.mu.Lock()
        uls.upstreamReady = false
        if uls.upstreamStarting {
            // Edge case: external stop while retry loop in progress — wake waiters.
            uls.upstreamStarting = false
            uls.upstreamReadyCond.Broadcast()
        }
        uls.mu.Unlock()
    }
    if len(info.Dependants) > 0 {
        go s.cascadeStop(info)
    }
}
```

### `proxy/udp.go` — new fields and constants

Remove:
```go
const (
    udpFirstDatagramRetries  = 10
    udpFirstDatagramInterval = 500 * time.Millisecond
)
```

Add:
```go
const udpReadinessInterval = 500 * time.Millisecond
```

New fields on `udpListenerState`:
```go
startTimeout      time.Duration
upstreamReady     bool        // set true after first successful response; reset on container stop
upstreamStarting  bool        // true while the leader goroutine holds the retry loop
upstreamReadyCond *sync.Cond  // broadcast when upstreamStarting → false
```

### `proxy/udp.go` — rewritten `startUDPFlow` readiness section

The function keeps the existing setup (EnsureRunning, GetUpstreamHost, ResolveUDPAddr, DialUDP) then
branches on the three readiness roles:

```go
// --- Readiness role selection ---
uls.mu.Lock()
isLeader := false
switch {
case uls.upstreamReady:
    // Fast path: already confirmed ready this session.
    uls.mu.Unlock()
case uls.upstreamStarting:
    // Follower: wait for the leader's outcome.
    for uls.upstreamStarting && !uls.removed {
        uls.upstreamReadyCond.Wait()
    }
    if !uls.upstreamReady || uls.removed {
        uls.mu.Unlock()
        upstreamConn.Close()
        cleanup()
        return
    }
    uls.mu.Unlock()
default:
    // Leader: this goroutine runs the readiness probe.
    uls.upstreamStarting = true
    isLeader = true
    uls.mu.Unlock()
}

if isLeader {
    retries := int((uls.startTimeout + udpReadinessInterval - 1) / udpReadinessInterval)
    if retries < 1 {
        retries = 1
    }
    // ... retry loop (send, read with 500ms deadline, retry on timeout/ECONNREFUSED) ...
    // On response: forward to client, set responded=true, break.

    uls.mu.Lock()
    uls.upstreamStarting = false
    if responded {
        uls.upstreamReady = true
    }
    uls.upstreamReadyCond.Broadcast()
    uls.mu.Unlock()

    if !responded {
        log.Printf("proxy: udp: upstream %s did not respond within %s; stopping container", ...)
        go s.backend.StopContainer(context.Background(), uls.info.ContainerID, uls.info.ContainerName)
        upstreamConn.Close()
        cleanup()
        return
    }
    // Register flow and start read loop (leader success path).
    // ...
    return
}

// Non-leader path: send first datagram directly (no retry loop).
// ...
// Register flow and start read loop.
```

`uls.lastActive` is set **only** inside the flow-registration block (after a successful response), never at dial time.

### Retry count calculation

```go
retries := int((uls.startTimeout + udpReadinessInterval - 1) / udpReadinessInterval)
if retries < 1 { retries = 1 }
```

| `START_TIMEOUT_SECS` | Retries | Total budget |
|----------------------|---------|--------------|
| 30 (default)         | 60      | 30 s         |
| 5                    | 10      | 5 s          |
| 10                   | 20      | 10 s         |

## Unit Tests

### `types_test.go` — `ParseStartTimeoutLabel`

| Test | Input | Expected |
|------|-------|----------|
| ValidPositive | `"30"` | `&30s` |
| Zero | `"0"` | `&0s` |
| WhitespaceAround | `"  60  "` | `&60s` |
| Empty | `""` | `nil` |
| WhitespaceOnly | `"   "` | `nil` |
| Negative | `"-5"` | `nil` |
| NonNumeric | `"abc"` | `nil` |

## Risks & Open Questions

- **`StopContainer` log says "idle timeout"**: The Docker and k8s backends log "idle timeout" or "scaled down (idle timeout)" in their `StopContainer` implementations. For a start-timeout stop this is cosmetically wrong. Fixing the backend log message is out of scope — the proxy-side log (`did not respond within %s; stopping container`) is sufficient to distinguish the two cases.
- **Minimum start timeout of 1 retry (500 ms)**: If `START_TIMEOUT_SECS=0` is set, `retries` clamps to 1 so the upstream gets exactly one 500 ms chance. This is an edge case unlikely to be used intentionally.
- **`checkInactivity` and pending flows**: Flows in `pending` already prevent idle-stop (`len(uls.pending) > 0`), so the container won't be idle-stopped while the leader is in the retry loop. No change needed there.
