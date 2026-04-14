# Fix UDP ECONNREFUSED Retry and Clarify Internal Flow Log Messages

**Date Added**: 2026-04-14
**Priority**: High
**Status**: Completed

## Problem Statement

When a UDP-only container (e.g. pihole DNS) was cold-started by an incoming datagram, the
first-datagram retry loop in `startUDPFlow` treated `ECONNREFUSED` (ICMP port-unreachable,
received when the DNS daemon had not yet bound to port 53) as a non-retryable fatal error and
broke out of the loop immediately. This silently dropped the first client query; the proxy
relied on the UDP client (e.g. `dig`) to time out and retransmit from a new source port.

Additionally, the log messages `proxy: udp: new flow from ...` and
`proxy: udp: flow ... expired` were ambiguous — they could be mistaken for client-facing
connection events rather than internal proxy-to-upstream flow lifecycle events.

## Functional Requirements

1. A `ECONNREFUSED` error on the upstream UDP read during the first-datagram retry loop must
   be treated as a retryable condition, identical to a deadline timeout.
2. The retry count and interval (`udpFirstDatagramRetries` = 10, `udpFirstDatagramInterval` =
   500 ms) apply equally to both timeout and `ECONNREFUSED` errors.
3. Log messages for new internal flows and expired flows must include the word "internal" and
   "attempt" to distinguish them from client-facing events.

## User Experience Requirements

- Users observing logs should be able to immediately distinguish proxy-internal upstream flow
  events from real client connection events.
- Cold-start DNS lookups (e.g. `dig ubuntu @pihole`) should succeed on the first attempt
  without requiring client-side retransmission.

## Technical Requirements

- The fix must be confined to `internal/proxy/udp.go`.
- No new configuration knobs are required.
- The change must not break existing timeout-based retry behaviour.

## Acceptance Criteria

- [x] `ECONNREFUSED` during first-datagram retry causes a retry log line and a retry, not an
      error break.
- [x] After exhausting all retries on `ECONNREFUSED`, the "did not respond after N attempts"
      message is logged and flow establishment continues (same as timeout exhaustion).
- [x] "new flow from" log line reads "new internal flow attempt from".
- [x] "flow … expired" log line reads "internal flow attempt … expired".
- [x] Build passes (`go build ./...`).

## Dependencies

- REQ-055 (Fix UDP First Packet Drop on Container Startup) — related; this fix extends the
  retry logic introduced there to cover `ECONNREFUSED` in addition to timeouts.

## Implementation Notes

Unified the retryability check into a single boolean:

```go
isRetryable := (func() bool {
    if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
        return true
    }
    return errors.Is(readErr, syscall.ECONNREFUSED)
})()
```

Added `"errors"` and `"syscall"` to imports. Log message strings updated in two places:
`udpReadLoop` (new flow) and `udpFlowSweeper` (expired flow).
