# Traefik TLS ALPN Protocols and Min Version — Implementation Plan

**Requirement**: [2026-10-17-traefik-tls-alpn-protocols-label.md](2026-10-17-traefik-tls-alpn-protocols-label.md)
**Date**: 2026-10-17
**Status**: Draft

## Implementation Steps

1. **`internal/types/types.go`** — add parsing helpers and `TargetInfo` fields:
   - `TargetInfo.ALPNProtocols []string` and `TargetInfo.MinTLSVersion string` (canonical `"1.0"|"1.1"|"1.2"|"1.3"` or `""`).
   - `ParseALPNProtocols(raw ...string) []string`: split each arg on `,`, trim, drop empties, de-duplicate keeping first-seen order, case preserved. Returns nil when empty.
   - `ParseMinTLSVersion(name, raw string) string`: trim; valid values returned as is; empty → `""`; anything else logs `container <name>: ignoring invalid min-version %q (must be 1.0, 1.1, 1.2, or 1.3)` and returns `""`.
2. **`internal/docker/manager.go`** — in both TargetInfo-building paths (inspect around line 191 and labels around line 790) read `lazy-tcp-proxy.alpn-protocols` and `lazy-tcp-proxy.min-version` and set the new `TargetInfo` fields (near lines 310 and 879). These labels are **not** added to the "missing required label" check.
3. **`internal/k8s/backend.go`** — read the same two annotations and set the fields (near line 392).
4. **`internal/config/store.go`** — add `ALPNProtocols []string` (`yaml:"alpn_protocols,omitempty"`) and `MinVersion string` (`yaml:"min_version,omitempty"`) to the entry struct; map into `TargetInfo` using the same parse helpers (each list item may itself be comma-separated).
5. **`internal/proxy/server.go`** — add `ALPNProtocols []string` and `MinTLSVersion string` (both `omitempty` JSON) to `TargetSnapshot`; populate in both TCP (~446) and UDP (~478) snapshot builders; extend the `reflect.DeepEqual` change-detection (~775) so label changes are noticed.
6. **`internal/traefik/config.go`**:
   - `Snapshot` gains `ALPNProtocols []string`, `MinTLSVersion string`.
   - New types: `TLSConfig{Options map[string]TLSOption}` and `TLSOption{AlpnProtocols []string; MinVersion string}` with `omitempty` JSON tags; `TraefikConfig.TLS *TLSConfig` (`json:"tls,omitempty"`).
   - `BuildConfig` gains a trailing parameter `extraALPN []string` (from the env var). After building routers: collect extras = env + all snapshots' protocols, drop any that are already Traefik defaults, de-duplicate, sort; `alpn = [h2, http/1.1, acme-tls/1] + extras` only when extras is non-empty. `minVersion` = highest valid snapshot value mapped to `VersionTLS1x`. If neither alpn nor minVersion is set, `TLS` stays nil.
   - Note: `minVersion` alone leaves `AlpnProtocols` empty (omitted), which keeps Traefik's defaults.
7. **`main.go`** — `resolveTraefikALPNProtocols()` reads `TRAEFIK_ALPN_PROTOCOLS` via `types.ParseALPNProtocols`; pass it through `runStatusServer` to `BuildConfig`; copy the two new snapshot fields in the `/traefik` handler.
8. **Docs** — `README_LABELS.md`: label rows, a "TLS options" section (global `default` semantics, conflict warning with user-defined `tls.options.default`, Postgres direct-TLS example, strictest-wins for `min-version`). `README.md`: `TRAEFIK_ALPN_PROTOCOLS` row. Update `QANDA.md` only if it already covers Traefik.
9. **Tests**, then `golangci-lint run` and `go test ./...` (build `html/dist` first: `cd html && npm ci && npm run build`).
10. Mark REQ-116 Completed, plan Implemented, record deviations, commit and push; open a PR (no local merge to main).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | `TargetInfo` fields, `ParseALPNProtocols`, `ParseMinTLSVersion` |
| `internal/types/types_test.go` | Modify | Parser tests |
| `internal/docker/manager.go` | Modify | Read two labels in both paths |
| `internal/k8s/backend.go` | Modify | Read two annotations |
| `internal/config/store.go` | Modify | `alpn_protocols`, `min_version` keys |
| `internal/proxy/server.go` | Modify | `TargetSnapshot` fields, change detection |
| `internal/traefik/config.go` | Modify | `tls.options.default` emission |
| `internal/traefik/config_test.go` | Modify | New tests; existing `BuildConfig` calls get `nil` for `extraALPN` |
| `main.go` | Modify | Env var, wiring |
| `README.md`, `README_LABELS.md` | Modify | Documentation |
| `requirements/_index.md` | Modify | Status |

## API Contracts

`GET /traefik` — new optional top-level section, only when configured:
```json
"tls": {
  "options": {
    "default": {
      "alpnProtocols": ["h2", "http/1.1", "acme-tls/1", "postgresql"],
      "minVersion": "VersionTLS13"
    }
  }
}
```
Absent entirely when no ALPN protocols or min-version are configured.

## Key Code Snippets

```go
var traefikDefaultALPN = []string{"h2", "http/1.1", "acme-tls/1"}

var minVersionNames = map[string]string{
	"1.0": "VersionTLS10", "1.1": "VersionTLS11",
	"1.2": "VersionTLS12", "1.3": "VersionTLS13",
}
```
Highest version: compare the canonical strings via the numeric order of the map keys (`1.0 < 1.1 < 1.2 < 1.3` also sorts correctly lexicographically).

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| ParseALPNProtocols basic | `"h2,postgresql"` | `[h2 postgresql]` |
| ParseALPNProtocols normalise | `" postgresql , ,postgresql"` | `[postgresql]` |
| ParseALPNProtocols empty | `""` | `nil` |
| ParseALPNProtocols case kept | `"PostgreSQL"` | `[PostgreSQL]` |
| ParseMinTLSVersion valid | `"1.3"` | `"1.3"` |
| ParseMinTLSVersion invalid | `"tls13"` | `""` (log) |
| BuildConfig no TLS settings | snapshots without ALPN/min | `TLS == nil`, JSON has no `"tls"` key |
| BuildConfig single label | one snap `[postgresql]` | alpn `[h2 http/1.1 acme-tls/1 postgresql]` |
| BuildConfig aggregation | `[h2 postgresql]` + `[postgresql custom]` | `[h2 http/1.1 acme-tls/1 custom postgresql]` |
| BuildConfig env merge | env `[postgresql]` + snap `[custom]` | defaults + `custom postgresql` |
| BuildConfig only defaults | snap `[h2]` | `TLS == nil` for ALPN (no extras) |
| BuildConfig determinism | two runs, shuffled snapshots | identical JSON |
| BuildConfig min-version highest | `1.2` and `1.3` | `VersionTLS13` |
| BuildConfig min-version only | `1.3`, no ALPN | `minVersion` set, `alpnProtocols` omitted |
| Docker label parse | container labels set | `TargetInfo.ALPNProtocols`/`MinTLSVersion` populated |
| K8s annotation parse | annotations set | same |
| Config store parse | YAML `alpn_protocols`, `min_version` | same |
| Change detection | label change on a target | snapshots compare unequal |

## Risks & Open Questions

- **Global effect:** ALPN and `min-version` apply to every TLS router on the entrypoint (`default` option). Documented; a per-router named option is a future requirement (also needed for `clientAuth`).
- **Conflict with user config:** a user-defined `tls.options.default` in another provider conflicts; Traefik logs it. Documented.
- **`min-version` strictest-wins** means a stray `1.3` label changes behaviour for unrelated services; documented prominently.
- **Cron-only targets** have no listen ports; their labels are included only if they are in the proxy snapshot. If a cron-only container sets `alpn-protocols`, it will be ignored; confirm this is acceptable.
- **No live Traefik test** is possible in CI; I will add a JSON-shape assertion matching Traefik's documented `tls.options` schema, and validate manually if a Traefik container is reachable.
