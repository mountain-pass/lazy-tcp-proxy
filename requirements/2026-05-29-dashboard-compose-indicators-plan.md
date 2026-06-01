# Dashboard Compose & Image Archive Indicators — Implementation Plan

**Requirement**: [2026-05-29-dashboard-compose-indicators.md](2026-05-29-dashboard-compose-indicators.md)
**Date**: 2026-05-29
**Status**: Implemented

---

## Implementation Steps

1. **Add `HasComposeFile` and `HasTarGz` to `TargetSnapshot`** (`internal/proxy/server.go`)
   ```go
   HasComposeFile bool `json:"has_compose_file"`
   HasTarGz       bool `json:"has_tar_gz"`
   ```
   No `omitempty` — always serialised so JS can read unconditionally.

2. **Add `composeDir` field and `SetComposeDir` method to `ProxyServer`** (`internal/proxy/server.go`)
   ```go
   // in ProxyServer struct
   composeDir string

   // new method
   func (s *ProxyServer) SetComposeDir(dir string) { s.composeDir = dir }
   ```

3. **Add `composeFlags` helper to `ProxyServer`** (`internal/proxy/server.go`)
   New private method; checks `os.Stat` for compose and tar.gz files:
   ```go
   func (s *ProxyServer) composeFlags(name string) (hasCompose, hasTar bool) {
       if s.composeDir == "" {
           return false, false
       }
       for _, ext := range []string{".yml", ".yaml"} {
           if _, err := os.Stat(filepath.Join(s.composeDir, name+ext)); err == nil {
               hasCompose = true
               break
           }
       }
       if _, err := os.Stat(filepath.Join(s.composeDir, name+".tar.gz")); err == nil {
           hasTar = true
       }
       return
   }
   ```
   Requires adding `"os"` and `"path/filepath"` to the import block.

4. **Populate the new fields in `Snapshot()`** (`internal/proxy/server.go`)
   In both the TCP and UDP snapshot loops, call `composeFlags` and assign:
   ```go
   hasCompose, hasTar := s.composeFlags(ts.info.ContainerName)
   // ...
   HasComposeFile: hasCompose,
   HasTarGz:       hasTar,
   ```

5. **Wire `srv.SetComposeDir` in `main.go`**
   Add one line immediately after `mgr.SetComposeDir(composeDir)` (line ~504):
   ```go
   srv.SetComposeDir(composeDir)
   ```

6. **Update the status dashboard HTML/JS** (`main.go` — the `statusDashboardHTML` constant)

   **a) `statusIcon` function** — wrap each emoji in a `<span title="...">`:
   ```js
   function statusIcon(snap) {
     if (snap.container_missing) return '<span title="Container missing">⚠️</span>';
     if (!snap.running)          return '<span title="Container stopped">🔴</span>';
     return snap.active_conns > 0
       ? '<span title="Container running">🟢</span>'
       : '<span title="Container idle">🟠</span>';
   }
   ```

   **b) New `composeIcons` function** — renders ♻️ and 📦 with titles and greyed opacity:
   ```js
   function composeIcons(snap) {
     const recycleTitle = snap.has_compose_file ? 'Compose file found' : 'No compose file';
     const boxTitle     = snap.has_tar_gz       ? 'Docker image tar found' : 'No docker image tar';
     const recycleStyle = snap.has_compose_file ? '' : ' style="opacity:0.25"';
     const boxStyle     = snap.has_tar_gz       ? '' : ' style="opacity:0.25"';
     return '<span title="' + recycleTitle + '"' + recycleStyle + '>♻️</span>' +
            '<span title="' + boxTitle     + '"' + boxStyle     + '>📦</span>';
   }
   ```

   **c) Update `targetCell`** — insert `composeIcons(snap)` between the status icon and the name:
   ```js
   const targetCell = statusIcon(snap) + composeIcons(snap) +
     ' ' + esc(snap.container_name) + ':' + snap.target_port + udp;
   ```

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | Add `HasComposeFile`/`HasTarGz` to snapshot; add `composeDir` field, `SetComposeDir`, `composeFlags`; populate in `Snapshot()` |
| `lazy-tcp-proxy/main.go` | Modify | Wire `srv.SetComposeDir`; update dashboard JS (`statusIcon`, new `composeIcons`, `targetCell`) |

---

## Key Code Snippets

### Resulting target column HTML for a service with both files
```html
<span title="Container stopped">🔴</span><span title="Compose file found">♻️</span><span title="Docker image tar found">📦</span> multilit-pdf-generator:3002
```

### Greyed icon (no file)
```html
<span title="No compose file" style="opacity:0.25">♻️</span>
```

---

## Risks & Open Questions

- None. This is a self-contained frontend + snapshot change with no new dependencies.
