# Traefik TLS ALPN Protocols and Min Version via Container Labels

**Date Added**: 2026-10-17
**Priority**: Medium
**Status**: In Progress

## Problem Statement

Postgres 17+ clients using direct TLS (`sslnegotiation=direct`) offer ALPN `postgresql` in the TLS ClientHello. Traefik's default TLS options only advertise `h2`, `http/1.1` and `acme-tls/1`, so Traefik aborts the handshake with `tlsv1 alert no application protocol`. lazy-tcp-proxy's `/traefik` endpoint cannot fix this today: `RouterTLS` only carries `certResolver`, and the payload has no top-level `tls` section, so users must maintain a separate Traefik file-provider config with `tls.options.default.alpnProtocols`.

## Functional Requirements

1. New label `lazy-tcp-proxy.alpn-protocols` — comma-separated ALPN protocol names (e.g. `postgresql` or `h2,postgresql`).
2. lazy-tcp-proxy collects the label across **all** discovered targets, trims whitespace, drops empty entries, de-duplicates, and emits the result as a single `tls.options.default.alpnProtocols` list in the `/traefik` payload.
3. The emitted list always begins with Traefik's built-in defaults (`h2`, `http/1.1`, `acme-tls/1`), followed by the extra protocols in sorted order. Supplying `alpnProtocols` *replaces* Traefik's defaults, so omitting them would break HTTP routers and ACME on the same entrypoint.
4. When no target sets the label, the `/traefik` payload is unchanged (no `tls` section) — zero behaviour change for existing users.
5. Protocol names are matched case-sensitively as written (ALPN IDs are case-sensitive); no case folding.
6. The label is also accepted as a Kubernetes annotation (`lazy-tcp-proxy.alpn-protocols`) and a config-store key (`alpn_protocols`).
7. New env var `TRAEFIK_ALPN_PROTOCOLS` (comma-separated) is merged into the same de-duplicated list, so protocols can be enabled globally without labelling containers.
8. New label `lazy-tcp-proxy.min-version` (`1.0`, `1.1`, `1.2` or `1.3`; also annotation and config-store key `min_version`). Emitted as `tls.options.default.minVersion` (`VersionTLS10`…`VersionTLS13`). Across containers the **highest** (strictest) value wins. Invalid values are logged and ignored. Because it lands in the global `default` option, one container requesting `1.3` raises the minimum for every TLS router on the entrypoint; this is documented.

## User Experience Requirements

- Postgres via Traefik works with a single label, no separate Traefik file config:
  ```yaml
  labels:
    - "lazy-tcp-proxy.enabled=true"
    - "lazy-tcp-proxy.traefik-tcp-hosts=postgres.example.com:5432"
    - "lazy-tcp-proxy.alpn-protocols=postgresql"
  ```
- README_LABELS.md documents the label, the "global default option" semantics, and the conflict caveat below.

## Technical Requirements

- Traefik TLS options are selected by SNI at handshake time, and the generated routers cannot name an option, so the aggregated list is emitted as `default`. It therefore applies to **every** TLS router on the entrypoint, not only the labelled container's host. This is why a union across containers is the right model.
- Extend `TraefikConfig` with an optional `TLS *TLSConfig` (`json:"tls,omitempty"`) containing `Options map[string]TLSOption`; `TLSOption` has `AlpnProtocols []string` (`json:"alpnProtocols,omitempty"`).
- Add `AlpnProtocols []string` to `traefik.Snapshot` and `types.TargetInfo`; populate from Docker labels, Kubernetes annotations, and the config-store entry (YAML key `alpn_protocols`) in line with how `traefik_tcp_hosts` is handled.
- Output must be deterministic (sorted extras) so the Traefik HTTP provider does not see spurious config changes between polls.
- Only one provider may define `default` TLS options. If the user also defines `tls.options.default` in a Traefik file provider, Traefik reports a conflict. Document that users should use *either* this label *or* their own `default`.

## Acceptance Criteria

- [ ] A container with `lazy-tcp-proxy.alpn-protocols=postgresql` causes `/traefik` to include `tls.options.default.alpnProtocols = ["h2","http/1.1","acme-tls/1","postgresql"]`.
- [ ] Two containers with `h2,postgresql` and `postgresql,custom` produce one de-duplicated list `[h2, http/1.1, acme-tls/1, custom, postgresql]`.
- [ ] Whitespace and empty entries (`" postgresql , ,"`) are normalised.
- [ ] With no label anywhere, `/traefik` output has no `tls` key (byte-for-byte unchanged for existing tests).
- [ ] The label works from Docker labels, K8s annotations and the config store.
- [ ] Output ordering is stable across repeated calls.
- [ ] `lazy-tcp-proxy.min-version=1.3` emits `minVersion: VersionTLS13`; with several containers the highest wins; invalid values are ignored with a log line.
- [ ] `TRAEFIK_ALPN_PROTOCOLS` is merged with label values.
- [ ] Unit tests cover parsing, aggregation, de-duplication, ordering, and absence of the section; `golangci-lint run` and `go test ./...` pass.
- [ ] README_LABELS.md documents the label; the Postgres direct-TLS usage is described.

## Dependencies

- Builds on the `/traefik` HTTP provider endpoint and REQ-101 (`traefik-hosts` / `traefik-tcp-hosts`).
- Related: REQ-115 (postgres-ssl recipe).

## Other TLS Options Considered

Traefik `tls.options` also supports the settings below. `minVersion` is now **in scope** (label only, as agreed); the rest are **out of scope**:

| Option | Use | Fit as a label |
|--------|-----|----------------|
| `minVersion` / `maxVersion` | Enforce TLS 1.2/1.3 | Poor — one global value; unsafe to merge across containers (strictest wins would surprise others). Better as an env var (`TRAEFIK_TLS_MIN_VERSION`). |
| `cipherSuites`, `curvePreferences` | Cipher hardening | Global security policy — env var if anything. |
| `sniStrict` | Reject requests with no matching cert instead of serving the default cert | Global — env var. Relevant here because IP-host connections currently receive Traefik's default cert. |
| `clientAuth` (mTLS: `caFiles`, `clientAuthType`) | Require client certificates for DB access | Genuinely per-service, but needs a *named* option referenced from the router, which the generated routers can't do today. A separate, larger requirement. |

## Decisions (from review)

1. Label name `lazy-tcp-proxy.alpn-protocols`; also supported as K8s annotation and config-store key — **confirmed**.
2. Traefik default protocols are auto-prepended — **confirmed**.
3. Global env var `TRAEFIK_ALPN_PROTOCOLS` merged into the same list — **confirmed**.
4. Add `lazy-tcp-proxy.min-version=1.3` only (no other TLS options now) — **confirmed**.

## Implementation Notes

To be completed in the Plan phase.
