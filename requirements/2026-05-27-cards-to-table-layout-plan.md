# Status Dashboard — Cards to Table Layout — Implementation Plan

**Requirement**: [2026-05-27-cards-to-table-layout.md](2026-05-27-cards-to-table-layout.md)
**Date**: 2026-05-27
**Status**: Approved

## Implementation Steps

1. **`internal/proxy/server.go` — extend `TargetSnapshot`**
   Add three new exported fields after `TraefikTCPHosts`:
   ```go
   IsUDP        bool   `json:"is_udp"`
   HasAuth      bool   `json:"has_auth"`
   Availability string `json:"availability"`
   ```
   `TLSEnabled` is not added — it is not needed for the dashboard display.

2. **`internal/proxy/server.go` — update `Snapshot()` for TCP entries**
   Populate the three new fields on each TCP `TargetSnapshot`:
   - `IsUDP: false`
   - `HasAuth: len(ts.apiKey) > 0 || len(ts.basicAuth) > 0`
   - `Availability: types.EffectiveAvailability(ts.info)`

3. **`internal/proxy/server.go` — add UDP entries to `Snapshot()`**
   After the TCP loop, add a UDP loop over `s.udpTargets`.  For each `uls`:
   - Capture `lastActive` under `uls.mu` (same pattern as `checkInactivity`).
   - If zero, fall back to `s.startTime`.
   - Emit a `TargetSnapshot` with:
     - `IsUDP: true`, `HasAuth: false`
     - `ActiveConns: uls.activeFlows.Load()`
     - `Availability: types.EffectiveAvailability(uls.info)`
     - `Running: uls.running`
     - Same `ContainerID` truncation (12 chars) as TCP path.

4. **`internal/proxy/server.go` — update `sort.Slice` in `Snapshot()`**
   Change the secondary-sort key from `ContainerID` to a three-key comparison:
   container name → `IsUDP` (false before true, i.e. TCP first) → `ListenPort`.

5. **`main.go` — replace `statusDashboardHTML`**
   Rewrite the constant with a table layout. Changes:
   - Remove the card CSS (`.container-card`, `.status-badge`, `.ports`, etc.).
   - Add table CSS (dark-themed `<table>`, `<th>`, `<td>` rules; horizontal scroll on narrow).
   - Replace the `render()` JS function body:
     - Iterate `data` directly (rows are already one-per-port, sorted).
     - For each `snap`, compute the DNS cell by filtering `traefik_hosts` /
       `traefik_tcp_hosts` where the port suffix equals `snap.listen_port`.
       Always use `https://domain` for `traefik_hosts`;
       always use `tcp://domain` for `traefik_tcp_hosts`.
       Cell is blank when no matching entry exists in either list.
     - Proxy cell: `(snap.has_auth ? '🔒' : '🔓') + ' :' + snap.listen_port +
       (snap.is_udp ? '/udp' : '') + ' [' + snap.availability + ']'`
     - Target cell: status icon + `snap.container_name + ':' + snap.target_port +
       (snap.is_udp ? '/udp' : '')`
     - Connections cell: `snap.active_conns`
   - Keep the 2-second `setInterval`, `last-updated` div, and error div.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | Add 4 fields to `TargetSnapshot`; update TCP loop; add UDP loop; update sort |
| `lazy-tcp-proxy/main.go` | Modify | Replace `statusDashboardHTML` with table layout |

---

## Key Code Snippets

### `TargetSnapshot` additions (server.go)

```go
type TargetSnapshot struct {
    ContainerID        string     `json:"container_id"`
    ContainerName      string     `json:"container_name"`
    ListenPort         int        `json:"listen_port"`
    TargetPort         int        `json:"target_port"`
    Running            bool       `json:"running"`
    ActiveConns        int32      `json:"active_conns"`
    LastActive         *time.Time `json:"last_active"`
    LastActiveRelative string     `json:"last_active_relative"`
    TraefikHosts       []string   `json:"traefik_hosts,omitempty"`
    TraefikTCPHosts    []string   `json:"traefik_tcp_hosts,omitempty"`
    IsUDP              bool       `json:"is_udp"`
    HasAuth            bool       `json:"has_auth"`
    Availability       string     `json:"availability"`
}
```

### UDP snapshot loop (server.go — inside `Snapshot()`)

```go
for listenPort, uls := range s.udpTargets {
    uls.mu.Lock()
    lastAct := uls.lastActive
    uls.mu.Unlock()
    if lastAct.IsZero() {
        lastAct = s.startTime
    }
    t := lastAct
    id := uls.info.ContainerID
    if len(id) > 12 {
        id = id[:12]
    }
    out = append(out, TargetSnapshot{
        ContainerID:        id,
        ContainerName:      uls.info.ContainerName,
        ListenPort:         listenPort,
        TargetPort:         uls.targetPort,
        Running:            uls.running,
        ActiveConns:        uls.activeFlows.Load(),
        LastActive:         &t,
        LastActiveRelative: relativeTime(lastAct, now),
        TraefikHosts:       uls.info.TraefikHosts,
        TraefikTCPHosts:    uls.info.TraefikTCPHosts,
        IsUDP:        true,
        HasAuth:      false,
        Availability: types.EffectiveAvailability(uls.info),
    })
}
```

### Updated sort (server.go)

```go
sort.Slice(out, func(i, j int) bool {
    if out[i].ContainerName != out[j].ContainerName {
        return out[i].ContainerName < out[j].ContainerName
    }
    if out[i].IsUDP != out[j].IsUDP {
        return !out[i].IsUDP // TCP before UDP
    }
    return out[i].ListenPort < out[j].ListenPort
})
```

### DNS matching in JS (main.go render function)

```javascript
function dnsForPort(snap) {
  const entries = [];
  const port = snap.listen_port;
  for (const h of (snap.traefik_hosts || [])) {
    const idx = h.lastIndexOf(':');
    if (idx < 1 || parseInt(h.substring(idx + 1)) !== port) continue;
    entries.push('https://' + h.substring(0, idx));
  }
  for (const h of (snap.traefik_tcp_hosts || [])) {
    const idx = h.lastIndexOf(':');
    if (idx < 1 || parseInt(h.substring(idx + 1)) !== port) continue;
    entries.push('tcp://' + h.substring(0, idx));
  }
  return entries;
}
```

### Row rendering in JS (main.go render function)

```javascript
function statusIcon(snap) {
  if (!snap.running) return '🔴';
  return snap.active_conns > 0 ? '🟢' : '🟠';
}

function render(data) {
  const tbody = document.getElementById('proxy-table-body');
  if (!data.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No services registered.</td></tr>';
    return;
  }
  let html = '';
  for (const snap of data) {
    const udp = snap.is_udp ? '/udp' : '';
    const dnsLines = dnsForPort(snap)
      .map(u => '<div>' + esc(u) + '</div>').join('');
    const proxyCell = (snap.has_auth ? '🔒' : '🔓') +
      ' :' + snap.listen_port + udp +
      ' [' + esc(snap.availability) + ']';
    const targetCell = statusIcon(snap) +
      ' ' + esc(snap.container_name) + ':' + snap.target_port + udp;
    html +=
      '<tr>' +
      '<td class="col-dns">' + dnsLines + '</td>' +
      '<td class="col-proxy">' + proxyCell + '</td>' +
      '<td class="col-target">' + targetCell + '</td>' +
      '<td class="col-conns">' + snap.active_conns + '</td>' +
      '</tr>';
  }
  tbody.innerHTML = html;
}
```

---

## Risks & Open Questions

- **`uls.mu` contention**: The UDP loop in `Snapshot()` briefly locks each UDP
  listener state to read `lastActive`. This is safe (same pattern as `checkInactivity`)
  but adds lock acquisition per UDP listener. With typical listener counts (< 50) this
  is negligible.
- **UDP `HasAuth`**: UDP listeners don't currently support auth; field is hardcoded
  `false`. If auth is added to UDP in future, this field just needs to be wired up.
- **Sort stability**: `sort.Slice` is not stable in Go; entries with identical
  container name, UDP flag, and listen port are not possible (each listen port is unique
  per protocol), so this is not a concern.
