# Fix UDP Cold-Start Timeouts for Slow-Starting Upstreams (e.g. Pi-hole)

**Date Added**: 2026-04-14
**Priority**: High
**Status**: In Progress

## Problem Statement

When a lazy-started upstream (e.g. Pi-hole) takes longer than the current UDP
first-datagram retry budget (10 × 500 ms = 5 s) to become ready, three
problems occur:

1. **Budget too short**: Pi-hole's FTL DNS daemon takes ~5 s to initialise
   after `EnsureRunning` returns, leaving zero margin before the 5 s window
   expires. The first DNS query always fails.

2. **Retry storm**: DNS clients use a fresh ephemeral source port per retry.
   Each new source port is a distinct flow key (`IP:port`), so every client
   retry spawns its own independent `startUDPFlow` goroutine — each running
   its own full retry loop against an upstream that is still not ready.
   This creates a cascade of concurrent retry loops all hammering a container
   that is still starting.

3. **`uls.lastActive` poisoning**: Flows are currently registered in
   `uls.flows` (updating `uls.lastActive`) *before* the first-datagram retry
   loop, regardless of whether the upstream ever responds. Failed flows
   therefore keep the idle timer fresh, delaying idle shutdown long after
   all clients have given up.

## Functional Requirements

1. A configurable **start timeout** replaces the fixed retry constants:
   - Global default: `START_TIMEOUT_SECS` environment variable (default
     **30 seconds**).
   - Per-container override: `lazy-tcp-proxy.start-timeout-secs` label (seconds).
   - The retry interval stays fixed at 500 ms; the number of retries is
     derived as `ceil(startTimeout / 500ms)`.

2. While one `startUDPFlow` goroutine is in the first-datagram retry loop
   for a given listener, subsequent flows for the **same listener**
   (regardless of source port) MUST block on a shared wait rather than each
   running their own independent retry loop.
   - Readiness is a property of the upstream container, not the client.
   - Once the upstream responds, all waiting goroutines unblock and each
     sends its own first datagram directly (upstream is now confirmed ready).

3. If the start timeout is exhausted without a response:
   - The proxy calls `backend.Stop()` on the container, returning it to a
     clean stopped state so the next request can attempt a fresh cold start.
   - All goroutines that were blocked on the shared wait are unblocked and
     return early — they do **not** forward their first datagram.
   - The failed flows are **not** registered in `uls.flows`.
   - `uls.lastActive` is **not** updated for any of these failed flows.

4. `uls.lastActive` is only updated after the upstream successfully responds
   to the first datagram — never on flow creation.

## User Experience Requirements

- No change to external behaviour when the upstream starts quickly (< 500 ms).
- For slow upstreams (e.g. Pi-hole ~5 s), clients whose queries arrive
  during the start window are held rather than silently dropped; once
  Pi-hole is ready all held queries are forwarded.
- If the start timeout is reached, the container is stopped cleanly; the
  client's next retry will trigger a fresh cold start.
- The start timeout is documented in the README alongside
  `IDLE_TIMEOUT_SECS`.

## Technical Requirements

- Replace the `udpFirstDatagramRetries` / `udpFirstDatagramInterval`
  constants in `udp.go` with a `startTimeout time.Duration` field on
  `udpListenerState` (populated from the global default or per-container
  label at listener creation time).
- Add to `udpListenerState`:
  - `upstreamReady bool` — set to `true` after the first successful
    datagram response; reset to `false` when the container stops.
  - `upstreamStarting bool` — `true` while one goroutine holds the retry
    loop; used to block other goroutines on the shared wait.
  - `upstreamReadyCond *sync.Cond` — broadcast when `upstreamStarting`
    becomes `false` (success or timeout).
- `ProxyServer` gains a `startTimeout time.Duration` field (global default).
- `TargetInfo` (or the listener creation path) reads
  `lazy-tcp-proxy.start-timeout-secs` and stores it alongside
  `lazy-tcp-proxy.idle-timeout-secs`.
- The shared-wait must not deadlock if the listener is removed while
  goroutines are waiting (the `removed` flag should trigger an early
  broadcast/unblock).
- `backend.Stop()` is called on timeout (same call used by the idle-timeout
  path); the container transitions to stopped and will restart on the next
  request.

## Acceptance Criteria

- [ ] `START_TIMEOUT_SECS` env var is read; default is 30 s.
- [ ] `lazy-tcp-proxy.start-timeout-secs` label overrides the global default for
      that container.
- [ ] While one goroutine is in the first-datagram retry loop, additional
      `startUDPFlow` goroutines for the same listener block rather than
      starting their own retry loop.
- [ ] When the upstream responds, all blocked goroutines unblock and each
      sends its own first datagram directly.
- [ ] When the start timeout exhausts: `backend.Stop()` is called, all
      blocked goroutines return early without forwarding, and neither
      `uls.flows` nor `uls.lastActive` are updated for the failed flows.
- [ ] `uls.lastActive` is only updated after a successful first datagram
      response, never on flow creation.
- [ ] `upstreamReady` resets to `false` when the container stops, so the
      next cold start re-runs the readiness check.
- [ ] Goroutines blocked on the wait unblock cleanly if the listener is
      removed while they are waiting (no goroutine leak).
- [ ] README updated: `START_TIMEOUT_SECS` in the Environment Variables
      table; `lazy-tcp-proxy.start-timeout-secs` in the Container Label
      Configuration table; a note in the UDP Support section.
- [ ] Existing unit and integration tests pass.

## Dependencies

- `lazy-tcp-proxy/internal/proxy/udp.go` — primary change.
- `lazy-tcp-proxy/internal/proxy/server.go` — reset `upstreamReady` on
  container-stopped events; read `START_TIMEOUT_SECS`; pass `startTimeout`
  to listener creation.
- `lazy-tcp-proxy/internal/types/types.go` — add `StartTimeout` field to
  `TargetInfo` (or equivalent config struct).
- `README.md` — documentation updates.
- REQ-027 (UDP Traffic Support) — extends the existing UDP implementation.
- REQ-055 (Fix UDP First Packet Drop on Container Startup) — extends the
  retry mechanism introduced in REQ-055.
- REQ-037 (Per-Container Idle Timeout Label Override) — the
  `lazy-tcp-proxy.start-timeout-secs` label follows the same pattern.

## Implementation Notes

- The retry interval stays at 500 ms (tight feedback loop); the timeout
  governs the total budget. `retries = int(math.Ceil(startTimeout.Seconds() / 0.5))`
- `upstreamReadyCond` is constructed once at listener creation:
  `sync.NewCond(&uls.mu)`.  All paths that read/write `upstreamStarting` /
  `upstreamReady` must hold `uls.mu`.
- On timeout, `backend.Stop()` is fire-and-forget in a goroutine (same
  pattern as webhook calls) so the retry goroutine does not block waiting
  for the stop to complete.
- The `upstreamReady` flag means that once a container has successfully
  responded to *any* first datagram, subsequent cold starts (after a stop)
  will still run the readiness check (because the flag is reset on stop).
  But within a single running period, the second and later flows skip the
  retry loop entirely and send directly.
