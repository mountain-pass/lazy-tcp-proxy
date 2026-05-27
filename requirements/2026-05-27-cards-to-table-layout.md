# Status Dashboard — Cards to Table Layout

**Date Added**: 2026-05-27
**Priority**: Low
**Status**: Completed

## Problem Statement

The status dashboard at `/` currently renders each container as a card with port badges.
This layout is compact but hard to scan when there are many containers or ports — it
does not surface the key operational columns (DNS name, proxy config, target status,
connection count) in a consistent grid.

A table layout with fixed columns makes it fast to compare many services at a glance.

## Functional Requirements

1. Replace the card layout with a `<table>` whose columns are:
   `(dns)`, `(proxy)`, `(target)`, `(connections)`

2. Show **one row per port mapping** (TCP and UDP). UDP ports are currently omitted
   from the `/status` JSON; they must be included.

3. **dns column** — the Traefik host URL(s) that point to this specific listen port.
   - `http://` or `https://` prefix for entries in `traefik_hosts`
   - `tcp://` prefix for entries in `traefik_tcp_hosts`
   - When multiple hosts match the same listen port, show them stacked in one cell.
   - Blank when no Traefik host is configured for this port.

4. **proxy column** — three pieces joined in one cell:
   - Auth icon: `🔒` if `basic_auth` or `api_key` is configured, else `🔓`
   - Listen port: `:N` for TCP, `:N/udp` for UDP
   - Availability bracket: `[ondemand]`, `[cron]`, or `[manual]`
   (These match the effective availability mode added by REQ-083.)

5. **target column** — three pieces joined in one cell:
   - Status icon: `🟢` running with active connections, `🟠` running but idle,
     `🔴` stopped
   - Container name
   - Target port: `:N` for TCP, `:N/udp` for UDP

6. **connections column** — count of active TCP connections or active UDP flows.
   Show `0` rather than blank.

7. Rows for the same container appear consecutively (sorted by container name,
   then TCP ports ascending, then UDP ports ascending).

8. The "last updated" timestamp and fetch-error display are retained.

9. Containers with no port mappings at all are not shown (no zombie rows).

## User Experience Requirements

- Dark-themed table matching the existing colour palette (`#0f1117` background,
  `#1e2330` rows, etc.).
- Column headers shown at the top; sticky header optional but not required.
- Table rows have subtle separator lines between containers (or alternating row shading).
- `🔒`/`🔓` and status icons are rendered inline in monospace columns.
- The page continues to auto-refresh every 2 seconds with no page reload.
- On narrow screens the table should scroll horizontally rather than wrapping.

## Technical Requirements

- **`internal/proxy/server.go`**:
  - Add fields to `TargetSnapshot`:
    - `IsUDP bool` — `json:"is_udp"`
    - `HasAuth bool` — `json:"has_auth"` (true if `APIKey` or `BasicAuth` non-empty)
    - `TLSEnabled bool` — `json:"tls_enabled"`
    - `Availability string` — `json:"availability"` (effective: ondemand/cron/manual)
  - `Snapshot()` must also iterate `s.udpTargets` and emit a `TargetSnapshot` per
    UDP mapping. Active connection count for UDP = `uls.activeFlows.Load()`.

- **`main.go`** — replace `statusDashboardHTML` card rendering with a table.
  No new files or Go dependencies.

## Acceptance Criteria

- [ ] `GET /status` includes UDP port mappings in its JSON response.
- [ ] `GET /status` entries include `is_udp`, `has_auth`, `tls_enabled`,
      `availability` fields.
- [ ] `GET /` renders a `<table>` with four columns: dns, proxy, target, connections.
- [ ] Each TCP port mapping appears as one table row.
- [ ] Each UDP port mapping appears as one table row with `/udp` suffix on port numbers.
- [ ] The dns cell shows the Traefik host URL matched to the row's listen port, or
      is blank when no Traefik host is configured.
- [ ] The proxy cell shows the correct auth icon, listen port, and availability mode.
- [ ] The target cell shows the correct status icon, container name, and target port.
- [ ] The connections column shows the active connection/flow count.
- [ ] The page auto-refreshes every 2 seconds.
- [ ] `GET /status` and `GET /health` remain unaffected.

## Dependencies

- REQ-056 (Status Dashboard at /) — this requirement supersedes the card layout.
- REQ-083 (Container Availability Config) — provides `availability` field and
  `EffectiveAvailability()` which is now surfaced in the snapshot.

## Implementation Notes

- `TraefikHosts` and `TraefikTCPHosts` are stored as `"domain:listen_port"` strings.
  In the JS, parse the port suffix with `host.split(':').at(-1)` and compare to
  `snap.listen_port` to filter which traefik entries belong to each row.
- `https://` prefix is used when `tls_enabled` is true; `http://` otherwise.
- UDP active connections: use `activeFlows atomic.Int32` on `udpListenerState`.
- The existing Snapshot sort (by container name, then by container ID) is extended
  to secondary-sort by `listen_port` within a container.
