# Per-Service Traefik TLS Options (ALPN Protocols, Min Version) via Labels

**Date Added**: 2026-10-17
**Priority**: Medium
**Status**: In Progress

## Problem Statement

Postgres 17+ clients using direct TLS (`sslnegotiation=direct`) offer ALPN `postgresql` in the TLS ClientHello. Traefik's default TLS options only advertise `h2`, `http/1.1` and `acme-tls/1`, so Traefik aborts the handshake with `tlsv1 alert no application protocol`. lazy-tcp-proxy's `/traefik` endpoint cannot fix this today: `RouterTLS` only carries `certResolver`, and the payload has no `tls` section, so users must maintain a separate Traefik file-provider config.

## Functional Requirements

1. New label `lazy-tcp-proxy.alpn-protocols` — comma-separated ALPN protocol names (e.g. `postgresql`).
2. New label `lazy-tcp-proxy.min-version` — minimum TLS version: `1.0`, `1.1`, `1.2` or `1.3`.
3. Both are also accepted as Kubernetes annotations (same names) and config-store keys (`alpn_protocols`, `min_version`).
4. For each container that sets either value, lazy-tcp-proxy emits a **named** Traefik TLS option under `tls.options` in `/traefik`, and sets `tls.options: "<name>"` on **only that container's** generated routers (both its HTTP `traefik-hosts` routers and its TCP `traefik-tcp-hosts` routers). Other routers are unaffected; no `default` option is emitted.
5. ALPN handling per container: values are trimmed, empty entries dropped, de-duplicated (first-seen order kept, case preserved). The emitted `alpnProtocols` is Traefik's defaults (`h2`, `http/1.1`, `acme-tls/1`) followed by the container's additional protocols, because supplying `alpnProtocols` replaces Traefik's defaults. If the container's list adds nothing beyond the defaults, `alpnProtocols` is omitted.
6. `min-version` maps to `minVersion` `VersionTLS10`…`VersionTLS13`. Invalid values are logged and ignored.
7. Containers with identical settings share one named option. Option names are deterministic and derived from the settings (e.g. `lazy-tls-alpn-postgresql-min-1-3`).
8. When no container sets either label, the `/traefik` payload is unchanged (no `tls` section, no `options` on routers).
9. The labels only take effect on containers that have `traefik-hosts` / `traefik-tcp-hosts` (that is what creates routers). On other containers they are accepted and ignored.

## User Experience Requirements

- Postgres via Traefik works with labels only, no separate Traefik file config:
  ```yaml
  labels:
    - "lazy-tcp-proxy.enabled=true"
    - "lazy-tcp-proxy.traefik-tcp-hosts=postgres.example.com:5432"
    - "lazy-tcp-proxy.alpn-protocols=postgresql"
    - "lazy-tcp-proxy.min-version=1.3"
  ```
- README_LABELS.md documents both labels, the per-service scope, and the constraints below.

## Technical Requirements

- Traefik selects TLS options by the SNI hostname in the handshake. A client that connects by bare IP sends no SNI and receives Traefik's `default` options, so the per-service option does not apply to it. Documented.
- Two routers on the same hostname with different options conflict in Traefik. Services are not expected to share hostnames; documented.
- Add `Options string` (`json:"options,omitempty"`) to `RouterTLS`; add `TraefikConfig.TLS *TLSConfig` with `Options map[string]TLSOption{AlpnProtocols []string; MinVersion string}`.
- Add `ALPNProtocols []string` / `MinTLSVersion string` to `types.TargetInfo`, `proxy.TargetSnapshot` and `traefik.Snapshot`; populate from labels, annotations and the config store.
- Output must be deterministic so the Traefik HTTP provider sees no spurious changes between polls.
- A router's `tls` block must still be emitted when only `options` is set (i.e. `TRAEFIK_CERTRESOLVER=""`).
- The four near-identical router-building blocks in `BuildConfig` repeat the `entryPoints` / `tls` setup; introduce small helpers for them while touching that code.

## Acceptance Criteria

- [ ] A container with `traefik-tcp-hosts` and `alpn-protocols=postgresql` produces `tls.options.<name>.alpnProtocols = ["h2","http/1.1","acme-tls/1","postgresql"]` and its TCP router has `tls.options = <name>`.
- [ ] Routers of other containers have no `options`; no `default` option is emitted.
- [ ] `min-version=1.3` emits `minVersion: VersionTLS13` for that container's routers only; two containers with different values get separate options.
- [ ] Identical settings on two containers share one option.
- [ ] Whitespace, empty and duplicate entries in `alpn-protocols` are normalised; invalid `min-version` is ignored with a log line.
- [ ] With no labels anywhere, `/traefik` output is unchanged (no `tls` key, no router `options`).
- [ ] Works with `TRAEFIK_CERTRESOLVER=""` (router still gets `tls.options`).
- [ ] Labels, annotations and config-store keys are all parsed.
- [ ] Output is stable across repeated calls.
- [ ] Validated against a real Traefik container: a direct-TLS ALPN `postgresql` handshake succeeds for the labelled host and an unlabelled host still rejects it.
- [ ] Unit tests added; `golangci-lint run` and `go test ./...` pass.
- [ ] README_LABELS.md updated.

## Dependencies

- Builds on the `/traefik` HTTP provider endpoint and REQ-101 (`traefik-hosts` / `traefik-tcp-hosts`).
- Related: REQ-115 (postgres-ssl recipe), REQ-114 (cron-only discovery — cron-only containers have no ports/routers so these labels do not apply to them).

## Other TLS Options Considered

Out of scope for now (not requested):

| Option | Use | Note |
|--------|-----|------|
| `sniStrict` | Reject handshakes with no matching cert instead of serving the default cert | Global in nature |
| `cipherSuites`, `curvePreferences` | Cipher hardening | Global security policy |
| `clientAuth` (mTLS) | Require client certs for DB access | Per-service; natural follow-on now that per-service named options exist |

## Decisions (from review)

1. Label `lazy-tcp-proxy.alpn-protocols`, also annotation and config-store key — confirmed.
2. Traefik default protocols auto-prepended — confirmed.
3. `lazy-tcp-proxy.min-version` added — confirmed.
4. **Per-service named options instead of a global `default` option — confirmed.**
5. **No `TRAEFIK_ALPN_PROTOCOLS` env var — removed.**
6. Services do not share hostnames, so the same-host option conflict is accepted.

## Implementation Notes

See the plan file.
