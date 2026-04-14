# Fix UDP Cold-Start Timeouts for Slow-Starting Upstreams (e.g. Pi-hole)

**Date Added**: 2026-04-14
**Priority**: High
**Status**: Planned

## Problem Statement

When a lazy-started upstream (e.g. Pi-hole) takes longer than the current UDP
first-datagram retry budget (10 × 500 ms = 5 s) to become ready, two problems
occur:

1. **Budget too short**: Pi-hole's FTL DNS daemon takes ~5 s to initialise
   after `EnsureRunning` returns, leaving zero margin before the 5 s window
   expires. The first DNS query always fails.

2. **Retry storm**: DNS clients use a fresh ephemeral source port per retry.
   Each new source port is a distinct flow key (`IP:port`), so every client
   retry spawns its own independent `startUDPFlow` goroutine — each running
   its own full 5-second retry loop against an upstream that is still not ready.
   This creates a cascade of failed flows. Additionally, each flow creation
   updates `uls.lastActive`, keeping the container alive and delaying idle
   shutdown long after all clients have given up.

## Functional Requirements

1. The UDP first-datagram retry budget MUST be increased to **30 retries ×
   1 s interval = 30 s** to cover slow-starting upstreams.

2. While one `startUDPFlow` goroutine is in the first-datagram retry loop for a
   given listener, subsequent flows for the **same listener** (regardless of
   source port) MUST share that wait rather than each running an independent
   retry loop.
   - The "ready" signal is per-listener (not per source IP/port), because
     readiness is a property of the upstream container, not the client.
   - Once the upstream responds (or the retry budget is exhausted), all waiting
     goroutines unblock and proceed.

## User Experience Requirements

- No change to external behaviour when the upstream starts quickly (< 1 s).
- For slow upstreams, the first DNS query from any client port that arrives
  while the retry loop is in progress should be queued and forwarded once the
  upstream is ready, rather than silently dropped.

## Technical Requirements

- Use a `sync.Cond` (or channel broadcast) on `udpListenerState` to signal
  readiness to waiting goroutines.
- A boolean flag (e.g. `upstreamReady`) on `udpListenerState` avoids re-running
  the retry loop once the upstream has been confirmed ready at least once.
- The retry constants in `udp.go` change to:
  ```go
  udpFirstDatagramRetries  = 30
  udpFirstDatagramInterval = 1 * time.Second
  ```
- The shared-wait mechanism must be safe for concurrent access (protected by
  `uls.mu`).
- When the retry budget is exhausted without a response the upstream is still
  considered "ready enough" for subsequent datagrams (existing behaviour), but
  the `upstreamReady` flag is also set so later flows don't retry again
  immediately.

## Acceptance Criteria

- [ ] `udpFirstDatagramRetries` is 30 and `udpFirstDatagramInterval` is 1 s.
- [ ] While one goroutine is in the first-datagram retry loop, additional
      `startUDPFlow` goroutines for the same listener block rather than
      starting their own retry loop.
- [ ] When the upstream responds, all blocked goroutines unblock and each sends
      its own first datagram directly (upstream is now ready).
- [ ] When the retry budget is exhausted, all blocked goroutines also unblock.
- [ ] `upstreamReady` is reset to `false` when the upstream container stops, so
      the next cold start re-runs the readiness check.
- [ ] Existing unit and integration tests pass unchanged.
- [ ] No goroutine leak: goroutines blocked on the wait unblock even if the
      listener is removed while they are waiting.

## Dependencies

- `lazy-tcp-proxy/internal/proxy/udp.go` — sole file of concern.
- `lazy-tcp-proxy/internal/proxy/server.go` — may need a hook to reset
  `upstreamReady` when a container stop event is received.
- REQ-027 (UDP Traffic Support) — this is an enhancement to the existing UDP
  implementation.
- REQ-055 (Fix UDP First Packet Drop on Container Startup) — this requirement
  extends the same retry mechanism introduced in REQ-055.

## Implementation Notes

The `upstreamReady` flag should be stored on `udpListenerState` and reset:
- To `false` when the proxy learns the container has stopped (via the existing
  container-stopped event path in `server.go`).
- It does NOT need to be reset when a flow is swept; flow sweeping is
  independent of container state.
