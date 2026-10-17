# Per-Service Traefik TLS Options — Implementation Plan

**Requirement**: [2026-10-17-traefik-tls-alpn-protocols-label.md](2026-10-17-traefik-tls-alpn-protocols-label.md)
**Date**: 2026-10-17
**Status**: Draft

## Implementation Steps

1. **`internal/types/types.go`** — fields and parsers:
   - `TargetInfo.ALPNProtocols []string`, `TargetInfo.MinTLSVersion string` (canonical `"1.0"|"1.1"|"1.2"|"1.3"` or `""`).
   - `ParseALPNProtocols(raw ...string) []string`: split each arg on `,`, trim, drop empties, de-duplicate keeping first-seen order, case preserved; nil when empty.
   - `ParseMinTLSVersion(name, raw string) string`: trim; valid → returned; empty → `""`; otherwise log `container <name>: ignoring invalid min-version %q (must be 1.0, 1.1, 1.2, or 1.3)` and return `""`.
2. **`internal/docker/manager.go`** — read `lazy-tcp-proxy.alpn-protocols` and `lazy-tcp-proxy.min-version` in both TargetInfo-building paths (inspect ~191 and labels ~790) and set the fields in the returned `TargetInfo` (~310, ~879). Not part of the "missing required label" check.
3. **`internal/k8s/backend.go`** — same two annotations; set fields (~392).
4. **`internal/config/store.go`** — add `ALPNProtocols []string` (`yaml:"alpn_protocols,omitempty"`) and `MinVersion string` (`yaml:"min_version,omitempty"`) to the entry struct; map via the parse helpers (list items may themselves be comma-separated).
5. **`internal/proxy/server.go`** — add `ALPNProtocols []string` and `MinTLSVersion string` (JSON `omitempty`) to `TargetSnapshot`; populate in the TCP (~446) and UDP (~478) snapshot builders; extend the `reflect.DeepEqual` change detection (~775) so label edits are noticed.
6. **`internal/traefik/config.go`**:
   - Types: `RouterTLS.Options string` (`json:"options,omitempty"`); `TLSConfig{Options map[string]TLSOption}`; `TLSOption{AlpnProtocols []string; MinVersion string}` (both `omitempty`); `TraefikConfig.TLS *TLSConfig` (`json:"tls,omitempty"`). `Snapshot` gains `ALPNProtocols`, `MinTLSVersion`.
   - Helpers (also removes four duplicated blocks): `routerEntryPoints(entryPoint string) []string` and `routerTLS(certResolver, options string) *RouterTLS` (nil when both empty).
   - `tlsOptionFor(snap Snapshot) (name string, opt TLSOption, ok bool)`: extras = ALPN list minus Traefik defaults; `alpnProtocols = defaults + extras` only if extras non-empty; `minVersion` from the version map; `ok=false` when neither is set. Name = `"lazy-tls"` + (`-alpn-` + sanitised extras joined by `-`) + (`-min-` + sanitised version, e.g. `1-3`), built with `sanitiseName`.
   - In `BuildConfig`, for each snapshot compute the option once; when `ok`, store it in a `tlsOptions` map (identical names collapse) and pass the name to `routerTLS` for that snapshot's HTTP and TCP routers. The `webHost` router never gets options. Set `cfg.TLS` only when `tlsOptions` is non-empty.
   - `BuildConfig`'s signature does **not** change.
7. **`main.go`** — copy the two new fields into `traefikpkg.Snapshot` in the `/traefik` handler. No new env vars.
8. **Docs** — `README_LABELS.md`: label rows and a "Per-service TLS options" section (SNI-based selection, no effect on bare-IP clients, same-hostname conflict, only applies to containers with Traefik hosts, Postgres direct-TLS example with `sslnegotiation=direct`). Mention in `README.md` only if its label table duplicates `README_LABELS.md`.
9. **Tests**, then `golangci-lint run` and `go test ./...` (build `html/dist` first: `cd html && npm ci && npm run build`).
10. **Live validation** (best effort; requires a working Docker daemon and image pulls): run `traefik` with only the HTTP provider pointing at a static server serving a generated `/traefik` JSON containing one labelled TCP router and one unlabelled TCP router (no certResolver → Traefik's default cert), then check with `openssl s_client -alpn postgresql -servername <host>` that the labelled host negotiates `postgresql` and the unlabelled host does not. This also confirms that a router can reference an option from the same provider without an `@http` suffix. If it cannot be run, say so in the implementation notes.
11. Mark REQ-116 Completed and the plan Implemented, record deviations, commit, push, open a PR (no local merge to `main`).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | `TargetInfo` fields, `ParseALPNProtocols`, `ParseMinTLSVersion` |
| `internal/types/types_test.go` | Modify | Parser tests |
| `internal/docker/manager.go` | Modify | Read two labels (both paths) |
| `internal/k8s/backend.go` | Modify | Read two annotations |
| `internal/config/store.go` | Modify | `alpn_protocols`, `min_version` keys |
| `internal/proxy/server.go` | Modify | `TargetSnapshot` fields, change detection |
| `internal/traefik/config.go` | Modify | Option types, helpers, per-snapshot options |
| `internal/traefik/config_test.go` | Modify | New tests |
| `main.go` | Modify | Copy snapshot fields |
| `README_LABELS.md` | Modify | Documentation |
| `requirements/_index.md` | Modify | Status |

## API Contracts

`GET /traefik` — new optional top-level `tls` section and per-router `tls.options`, only for labelled containers:

```json
"tcp": {
  "routers": {
    "postgres-example-com-8000-router": {
      "entryPoints": ["websecure"],
      "rule": "HostSNI(`postgres.example.com`)",
      "service": "postgres-example-com-8000-service",
      "tls": { "certResolver": "myresolver", "options": "lazy-tls-alpn-postgresql-min-1-3" }
    },
    "mongo-example-com-8007-router": {
      "entryPoints": ["websecure"],
      "rule": "HostSNI(`mongo.example.com`)",
      "service": "mongo-example-com-8007-service",
      "tls": { "certResolver": "myresolver" }
    }
  }
},
"tls": {
  "options": {
    "lazy-tls-alpn-postgresql-min-1-3": {
      "alpnProtocols": ["h2", "http/1.1", "acme-tls/1", "postgresql"],
      "minVersion": "VersionTLS13"
    }
  }
}
```

## Key Code Snippets

```go
var traefikDefaultALPN = []string{"h2", "http/1.1", "acme-tls/1"}

var minVersionNames = map[string]string{
	"1.0": "VersionTLS10", "1.1": "VersionTLS11",
	"1.2": "VersionTLS12", "1.3": "VersionTLS13",
}

func routerTLS(certResolver, options string) *RouterTLS {
	if certResolver == "" && options == "" {
		return nil
	}
	return &RouterTLS{CertResolver: certResolver, Options: options}
}
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| ParseALPNProtocols basic | `"h2,postgresql"` | `[h2 postgresql]` |
| ParseALPNProtocols normalise | `" postgresql , ,postgresql"` | `[postgresql]` |
| ParseALPNProtocols empty | `""` | `nil` |
| ParseALPNProtocols case kept | `"PostgreSQL"` | `[PostgreSQL]` |
| ParseMinTLSVersion valid | `"1.3"` | `"1.3"` |
| ParseMinTLSVersion invalid | `"tls13"` | `""` (log) |
| BuildConfig no settings | snapshots without ALPN/min | `TLS == nil`, no router `options`, JSON has no `"tls"` section |
| BuildConfig single ALPN | one TCP snap `[postgresql]` | option `lazy-tls-alpn-postgresql`; alpn `[h2 http/1.1 acme-tls/1 postgresql]`; router references it |
| BuildConfig scoped | labelled postgres snap + unlabelled mongo snap | only postgres router has `options`; no `default` option |
| BuildConfig shared | two snaps, same settings | one option, both routers reference it |
| BuildConfig distinct | `1.2` vs `1.3` | two options, each router references its own |
| BuildConfig only defaults | snap `[h2]`, no min | no option emitted |
| BuildConfig min only | `1.3`, no ALPN | `minVersion` set, `alpnProtocols` omitted, name `lazy-tls-min-1-3` |
| BuildConfig both | `postgresql` + `1.3` | single option with both fields |
| BuildConfig HTTP + TCP | one snap with HTTP and TCP hosts | both router kinds reference the option |
| BuildConfig no certResolver | `certResolver=""`, labelled | router `tls` = `{options: ...}` |
| BuildConfig webHost | `webHost` set with labelled snaps | web router has no `options` |
| BuildConfig determinism | repeated/shuffled input | identical JSON |
| Docker label parse | labels set | `TargetInfo` fields populated |
| K8s annotation parse | annotations set | same |
| Config store parse | YAML `alpn_protocols`, `min_version` | same |
| Change detection | label edit on a target | snapshots compare unequal |

## Risks & Open Questions

- **SNI-based selection:** a client connecting by bare IP gets Traefik's `default` options, not the service option. Documented.
- **Same hostname on two routers with different options** causes a Traefik conflict; accepted since services do not share hostnames.
- **Option reference format:** same-provider reference without `@http` is believed to work but is only confirmed by the live validation step. If it needs the suffix, router `options` become `<name>@http`.
- **Live Traefik test depends on environment** (Docker daemon, image pulls); if unavailable the JSON shape is covered by unit tests only, and I will say so.
- **Router `tls` with `options` but no `certResolver`** relies on Traefik's default/static certificates for that host; documented.
- **Helper refactor** touches four existing router blocks; covered by the existing config tests, which must stay green unchanged.
