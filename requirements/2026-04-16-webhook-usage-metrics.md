# Webhook Usage Metrics (bytes, duration, port)

**Date Added**: 2026-04-16
**Priority**: Medium
**Status**: In Progress

## Problem Statement

The current `tcp_conn_end` and `udp_flow_end` webhook payloads carry only connection identity
fields (connection ID, remote IP/port, container name). Consumers who want to use webhooks for
**usage tracking** (quota enforcement, billing, auditing) cannot do so because the payload lacks:

- how much data was transferred (bytes in each direction)
- how long the connection lasted
- which proxy listen port was used (needed when a single consumer uses multiple services)
- which upstream address was dialled

## Functional Requirements

1. `tcp_conn_end` webhook payload MUST include:
   - `bytes_sent` (int64) — bytes transferred from client → upstream
   - `bytes_received` (int64) — bytes transferred from upstream → client
   - `duration_ms` (int64) — wall-clock duration of the connection in milliseconds
   - `listen_port` (int) — the proxy's listen port the client connected to
   - `upstream_addr` (string) — the upstream host:port that was dialled
   - `started_at` (string, RFC3339) — UTC timestamp when the connection was accepted
   - `ended_at` (string, RFC3339) — UTC timestamp when the connection was closed

2. `tcp_conn_start` webhook payload MUST include:
   - `started_at` (string, RFC3339) — UTC timestamp when the connection was accepted
   - `listen_port` (int) — the proxy's listen port the client connected to

3. `udp_flow_end` webhook payload MUST include the same fields as `tcp_conn_end`
   (bytes_sent, bytes_received, duration_ms, listen_port, upstream_addr,
   started_at, ended_at), with the same semantics.

4. `udp_flow_start` webhook payload MUST include:
   - `started_at` (string, RFC3339) — UTC timestamp when the flow was registered
   - `listen_port` (int) — the proxy's listen port the client connected to

5. The metrics fields (`bytes_*`, `duration_ms`, `upstream_addr`, `ended_at`) MUST be
   present on the `*_end` events only. `started_at` and `listen_port` appear on both
   start and end events.

4. Existing fields on all events MUST remain unchanged (no renames, no removals).

5. README.md webhook documentation MUST be updated to document the new fields.

## User Experience Requirements

Webhook consumers (external services, scripts) receive richer JSON with no changes to
event names or existing fields. The additional fields appear alongside the current ones.

Example `tcp_conn_start` payload after this change:

```json
{
  "event": "tcp_conn_start",
  "connection_id": "a3f1c2d4-...",
  "remote_addr": "1.2.3.4",
  "remote_port": 51234,
  "container_id": "abc123def456",
  "container_name": "my-service",
  "timestamp": "2026-04-16T10:00:00Z",
  "listen_port": 8080,
  "started_at": "2026-04-16T10:00:00Z"
}
```

Example `tcp_conn_end` payload after this change:

```json
{
  "event": "tcp_conn_end",
  "connection_id": "a3f1c2d4-...",
  "remote_addr": "1.2.3.4",
  "remote_port": 51234,
  "container_id": "abc123def456",
  "container_name": "my-service",
  "timestamp": "2026-04-16T10:00:05Z",
  "listen_port": 8080,
  "upstream_addr": "172.17.0.3:80",
  "started_at": "2026-04-16T10:00:00Z",
  "ended_at": "2026-04-16T10:00:05Z",
  "bytes_sent": 1024,
  "bytes_received": 204800,
  "duration_ms": 4823
}
```

## Technical Requirements

- Byte counting must not require allocating a new buffer per connection; wrap the existing
  `io.CopyBuffer` calls with a lightweight counting writer that increments an atomic or
  mutex-protected counter.
- Duration is measured from just before `tcp_conn_start` fires to just before
  `tcp_conn_end` fires (i.e. covers the full lifetime of the proxied connection).
- `started_at` is captured once (before the start event fires) and stored alongside the
  connection ID so it can be included in both the start and end events.
- `ended_at` equals the timestamp captured immediately before `tcp_conn_end` fires;
  `duration_ms` = `ended_at - started_at` in milliseconds.
- `listen_port` is already available in `handleConn` via the port mapping; it just needs
  to be plumbed into the webhook call.
- `upstream_addr` is the address returned by the successful `net.Dial` call.
- For UDP: bytes and duration are tracked per flow (`udpFlow` struct). The sweep goroutine
  already has access to the flow when it fires `udp_flow_end`.
- No new external dependencies are permitted.

## Acceptance Criteria

- [ ] `tcp_conn_end` payload contains `bytes_sent`, `bytes_received`, `duration_ms`,
      `listen_port`, `upstream_addr`, `started_at`, and `ended_at` with correct values.
- [ ] `tcp_conn_start` payload contains `listen_port` and `started_at`.
- [ ] `udp_flow_end` payload contains the same seven fields with correct values.
- [ ] `udp_flow_start` payload contains `listen_port` and `started_at`.
- [ ] `container_started` and `container_stopped` payloads are unchanged.
- [ ] Existing integration tests continue to pass.
- [ ] README.md webhook section documents all new fields.

## Dependencies

- REQ-041 (Webhook Connection Events) — introduced `tcp_conn_start/end`
- REQ-044 (Webhook Connection Events — Add Source IP) — added `remote_addr/port`
- REQ-046 (UDP Flow Webhook Events) — introduced `udp_flow_start/end`

## Implementation Notes

The `webhookPayload` struct is defined in `lazy-tcp-proxy/internal/proxy/server.go`.
The `fireWebhook` function signature will need new parameters, or the struct should be
built by the caller and passed in directly (preferred, to avoid a long parameter list).
