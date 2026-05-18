# Per-Service HTTPS Termination and API Key Authentication

**Date Added**: 2026-05-15
**Priority**: High
**Status**: Completed

## Problem Statement

Traffic to proxied container services is currently unencrypted and unauthenticated at the proxy layer. Users who expose services publicly need a way to:
1. Encrypt traffic so it cannot be sniffed in transit (HTTPS termination at the proxy).
2. Restrict access so only callers with a shared secret can reach the service (API key auth).

Both features operate at the per-service level, configured via Docker labels or the YAML config file.

## Functional Requirements

### HTTPS Termination (`https` / `lazy-tcp-proxy.https`)

- When `https: true` is set on a service, the proxy listener for that service accepts TLS connections instead of plain TCP.
- The proxy terminates TLS and forwards decrypted bytes to the upstream container over plain TCP — the container itself needs no TLS configuration.
- A single self-signed certificate is generated at proxy startup (in memory; not written to disk) and shared across all `https: true` services.
- The certificate is regenerated each time the proxy restarts; no persistence or rotation is required.

### API Key Authentication (`api_key` / `lazy-tcp-proxy.api-key`)

- When `api_key: <value>` is set on a service, every inbound HTTP request must carry the header `X-API-Key: <value>`.
- The proxy reads and inspects the HTTP request headers before forwarding.
- If the header is absent or the value does not match: the proxy returns `HTTP 401 Unauthorized` and closes the connection. The request is **not** forwarded to the upstream container.
- If the header matches: the proxy strips the `X-API-Key` header and forwards the full HTTP request to the upstream container.
- `api_key` is independent of `https` — either can be used without the other.

### Combined usage

- `https: true` + `api_key`: the proxy accepts HTTPS connections, terminates TLS, checks the API key, then forwards plain HTTP to the container.
- `https: true` only: HTTPS accepted, decrypted, forwarded — no auth check.
- `api_key` only: plain HTTP accepted, API key checked, forwarded.
- Neither: existing TCP passthrough behaviour unchanged.

## User Experience Requirements

### Docker label configuration

```
lazy-tcp-proxy.https=true
lazy-tcp-proxy.api-key=ABCDEF
```

### YAML config file

```yaml
services:
  - name: "my-container"
    ports:
      - "9000:80"
    https: true
    api_key: ABCDEF
```

## Technical Requirements

- Self-signed cert generation uses the Go standard library (`crypto/tls`, `crypto/x509`, `crypto/ecdsa` or `crypto/rsa`). No third-party cert library required.
- The cert is generated once at startup and held in memory as a `tls.Certificate`. All `https: true` listeners share the same `tls.Config`.
- API key inspection requires parsing the first HTTP request on each connection (read headers, check `X-API-Key`, then either respond 401 or pipe remaining bytes + already-read bytes to upstream).
- The 401 response is a minimal valid HTTP/1.1 response: `HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\nConnection: close\r\n\r\n`.
- The `X-API-Key` header is removed from the forwarded request (not passed through to the container).
- Non-HTTP traffic on an `api_key`-only port (e.g. raw TCP) receives a 401 response and the connection is closed, since the proxy cannot parse headers.

## Acceptance Criteria

- [ ] `lazy-tcp-proxy.https=true` label causes the proxy to accept TLS on that service's port.
- [ ] Connecting with a plain TCP client to a `https: true` port fails (TLS handshake required).
- [ ] Connecting with a TLS client succeeds and traffic reaches the upstream container.
- [ ] `lazy-tcp-proxy.api-key=SECRET` label causes the proxy to require `X-API-Key: SECRET` on all HTTP requests.
- [ ] A request without `X-API-Key` receives `401 Unauthorized`.
- [ ] A request with the wrong key value receives `401 Unauthorized`.
- [ ] A request with the correct key is forwarded to the upstream container (with the header stripped).
- [ ] `https: true` + `api_key` together: HTTPS + auth both enforced.
- [ ] YAML config fields `https` and `api_key` work identically to the labels.
- [ ] Existing services with neither field are completely unaffected (pure TCP passthrough).
- [ ] Self-signed cert is generated at startup; the proxy log notes the cert's expiry date.

## Dependencies

- REQ-065 (Dynamic Configuration File) — `https` and `api_key` are added as fields to the existing YAML service schema.
- REQ-022 (Allow/Block Lists) — no conflict; these are additive per-service fields.

## Implementation Notes

- HTTP request buffering: read into a `bufio.Reader`, inspect headers, then replay buffered bytes + remaining stream to upstream (using `io.MultiReader`).
- For `https: true`, wrap the `net.Listener` with `tls.NewListener(l, tlsConfig)` before handing it to the existing proxy accept loop.
- TLS listener wrapping should happen in the same place existing listeners are created, keeping the accept loop unchanged.
