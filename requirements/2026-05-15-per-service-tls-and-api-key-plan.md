# Per-Service TLS Termination and API Key Authentication — Implementation Plan

**Requirement**: [2026-05-15-per-service-tls-and-api-key.md](2026-05-15-per-service-tls-and-api-key.md)
**Date**: 2026-05-15
**Status**: Draft

---

## Implementation Steps

### 1. Add `TLS` and `APIKey` fields to `TargetInfo` (`internal/types/types.go`)

Append two fields to the `TargetInfo` struct:

```go
TLS    bool   // true → wrap listener with TLS using shared self-signed cert
APIKey string // non-empty → require X-API-Key header on every HTTP request
```

### 2. Create `internal/proxy/tls.go` — self-signed cert generation

New file. Exports one function used by `main.go`:

```go
// GenerateSelfSignedTLSConfig generates an in-memory ECDSA P-256 self-signed
// certificate valid for 10 years and returns a *tls.Config that presents it.
// Logs the certificate expiry date on success.
func GenerateSelfSignedTLSConfig() (*tls.Config, error)
```

Implementation sketch:
- `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)`
- `x509.Certificate{SerialNumber, Subject, NotBefore: now, NotAfter: now+10yr, KeyUsage, ExtKeyUsage, BasicConstraintsValid}`
- `x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)` (self-signed)
- `tls.X509KeyPair(certPEM, keyPEM)` or `tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}`
- Return `&tls.Config{Certificates: []tls.Certificate{cert}}`
- Log: `proxy: TLS self-signed cert generated, expires %s`

### 3. Update `proxy.NewServer` to accept `*tls.Config` (`internal/proxy/server.go`)

Change signature:

```go
func NewServer(ctx context.Context, b containerBackend, startTime time.Time,
    idleTimeout, pollInterval, startTimeout time.Duration,
    tlsConfig *tls.Config) *ProxyServer
```

Add `tlsConfig *tls.Config` field to `ProxyServer`. Store the passed config.

### 4. Update `main.go` — generate cert and pass to `NewServer`

In `main.go`, before calling `proxy.NewServer`:

```go
tlsConfig, err := proxy.GenerateSelfSignedTLSConfig()
if err != nil {
    log.Fatalf("failed to generate self-signed TLS certificate: %v", err)
}
srv := proxy.NewServer(ctx, mgr, startTime, idleTimeout, tick, startTimeout, tlsConfig)
```

### 5. Add `tlsEnabled` and `apiKey` to `targetState` (`internal/proxy/server.go`)

```go
type targetState struct {
    // existing fields …
    tlsEnabled bool
    apiKey     string
}
```

### 6. Update `RegisterTarget` — wrap listener with TLS, store apiKey (`internal/proxy/server.go`)

**New target path** (inside `RegisterTarget`, `net.Listen` block):

```go
ln, err := net.Listen("tcp", fmt.Sprintf(":%d", m.ListenPort))
if err != nil { … }

if info.TLS {
    if s.tlsConfig == nil {
        log.Printf("proxy: TLS requested for %s but no TLS config available; skipping port %d",
            info.ContainerName, m.ListenPort)
        continue
    }
    ln = tls.NewListener(ln, s.tlsConfig)
}

ts := &targetState{
    // existing fields …
    tlsEnabled: info.TLS,
    apiKey:     info.APIKey,
}
```

**Update path** (same container, same port already registered):

```go
existing.apiKey = info.APIKey
existing.tlsEnabled = info.TLS
// Note: listener type cannot change in-place; TLS changes go through
// remove+register triggered by targetInfoEqual (see step 7).
```

### 7. Add `TLS` and `APIKey` to `targetInfoEqual` (`internal/proxy/server.go`)

Append to the `return` expression:

```go
a.TLS == b.TLS &&
a.APIKey == b.APIKey
```

This ensures a change in either field triggers a full remove + re-register of the listener (with or without TLS wrapping as appropriate).

### 8. Add HTTP proxy loop for API key enforcement (`internal/proxy/server.go`)

In `handleConn`, after the upstream `net.Conn` is established (after the retry dial loop) and before the bidirectional pipe section, insert:

```go
if ts.apiKey != "" {
    s.handleHTTPProxy(conn, upstream, ts)
    return
}
```

Add new method:

```go
// handleHTTPProxy handles a connection in HTTP mode: reads each request,
// enforces the X-API-Key header, strips it, and forwards to upstream.
// Supports HTTP/1.1 keep-alive. Called only when ts.apiKey is non-empty.
func (s *ProxyServer) handleHTTPProxy(client, upstream net.Conn, ts *targetState) {
    br  := bufio.NewReader(client)
    ubr := bufio.NewReader(upstream)

    for {
        req, err := http.ReadRequest(br)
        if err != nil {
            return // EOF or malformed request
        }

        if req.Header.Get("X-API-Key") != ts.apiKey {
            log.Printf("proxy: api-key: rejected request to \033[33m%s\033[0m from \033[36m%s\033[0m (bad or missing key)",
                ts.info.ContainerName, client.RemoteAddr())
            client.Write([]byte( //nolint:errcheck
                "HTTP/1.1 401 Unauthorized\r\n" +
                "Content-Length: 0\r\n" +
                "Connection: close\r\n\r\n"))
            return
        }
        req.Header.Del("X-API-Key")

        if err := req.Write(upstream); err != nil {
            return
        }
        if req.Body != nil {
            req.Body.Close() //nolint:errcheck
        }

        resp, err := http.ReadResponse(ubr, req)
        if err != nil {
            return
        }
        keepAlive := req.ProtoAtLeast(1, 1) && !resp.Close
        if err := resp.Write(client); err != nil {
            resp.Body.Close() //nolint:errcheck
            return
        }
        resp.Body.Close() //nolint:errcheck

        ts.lastActive = time.Now()

        if !keepAlive {
            return
        }
    }
}
```

### 9. Parse `lazy-tcp-proxy.tls` and `lazy-tcp-proxy.api-key` labels (`internal/docker/manager.go`)

In `containerToTargetInfo` (the function that reads Docker labels), add after the existing label reads:

```go
tls := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.tls"]) == "true"
apiKey := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.api-key"])
```

Set on the returned `TargetInfo`:

```go
TLS:    tls,
APIKey: apiKey,
```

### 10. Parse annotations in Kubernetes backend (`internal/k8s/backend.go`)

Mirror step 9 for the k8s annotation map `ann`:

```go
tls := strings.TrimSpace(ann["lazy-tcp-proxy.tls"]) == "true"
apiKey := strings.TrimSpace(ann["lazy-tcp-proxy.api-key"])
```

Set on the returned `TargetInfo`.

### 11. Add `TLS` and `APIKey` fields to `ServiceEntry` and wire them (`internal/config/store.go`)

In `ServiceEntry`:

```go
TLS    bool   `yaml:"tls,omitempty"     json:"tls,omitempty"`
APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
```

In `entryToTargetInfo`:

```go
info.TLS    = entry.TLS
info.APIKey = entry.APIKey
```

### 12. Update placeholder config comment in `store.go`

Add commented example lines for the two new fields in the placeholder YAML written when no config file exists:

```yaml
#    tls: true
#    api_key: "your-secret-key"
```

### 13. Add unit tests

**`internal/proxy/server_test.go`** — add cases to the existing `targetInfoEqual` tests:

| Test | Input A | Input B | Expected |
|------|---------|---------|----------|
| TLS differs | `TLS: false` | `TLS: true` | not equal |
| APIKey differs | `APIKey: "a"` | `APIKey: "b"` | not equal |
| Both same | `TLS: true, APIKey: "x"` | `TLS: true, APIKey: "x"` | equal |

**`internal/proxy/server_test.go`** — add integration-style test for `handleHTTPProxy` (or a dedicated `TestHandleHTTPProxy`):

| Test | Scenario | Expected |
|------|----------|----------|
| Correct key | `X-API-Key: secret` | request forwarded, 200 response returned |
| Missing key | no `X-API-Key` header | `401 Unauthorized` returned, connection closed |
| Wrong key | `X-API-Key: wrong` | `401 Unauthorized` returned, connection closed |
| Header stripped | correct key | upstream does not receive `X-API-Key` header |
| Keep-alive | two sequential requests, both with correct key | both forwarded, connection reused |

**`internal/proxy/tls_test.go`** (new file):

| Test | Expected |
|------|----------|
| `GenerateSelfSignedTLSConfig` succeeds | non-nil `*tls.Config` returned, cert parseable |
| Returned cert | valid ECDSA cert, `NotAfter` ≥ 9 years from now |

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | Add `TLS bool` and `APIKey string` to `TargetInfo` |
| `internal/proxy/tls.go` | Create | `GenerateSelfSignedTLSConfig()` |
| `internal/proxy/tls_test.go` | Create | Unit tests for cert generation |
| `internal/proxy/server.go` | Modify | `NewServer` signature; `targetState` fields; `RegisterTarget` TLS wrap; `targetInfoEqual`; `handleConn` branch; new `handleHTTPProxy` |
| `main.go` | Modify | Generate TLS config; pass to `NewServer` |
| `internal/docker/manager.go` | Modify | Parse `lazy-tcp-proxy.tls` and `lazy-tcp-proxy.api-key` labels |
| `internal/k8s/backend.go` | Modify | Parse `lazy-tcp-proxy.tls` and `lazy-tcp-proxy.api-key` annotations |
| `internal/config/store.go` | Modify | Add `TLS` and `APIKey` to `ServiceEntry`; wire in `entryToTargetInfo`; update placeholder |
| `internal/proxy/server_test.go` | Modify | Add `targetInfoEqual` cases; add `handleHTTPProxy` tests |

---

## API Contracts

No new HTTP endpoints. New per-service configuration fields only.

**Docker label:**
```
lazy-tcp-proxy.tls=true
lazy-tcp-proxy.api-key=<secret>
```

**YAML config:**
```yaml
tls: true
api_key: <secret>
```

**401 response format** (exact bytes written to client):
```
HTTP/1.1 401 Unauthorized\r\n
Content-Length: 0\r\n
Connection: close\r\n
\r\n
```

---

## Key Code Snippets

### TLS listener wrapping in `RegisterTarget`

```go
ln, err := net.Listen("tcp", fmt.Sprintf(":%d", m.ListenPort))
if err != nil {
    log.Printf("proxy: failed to listen on TCP port %d for \033[33m%s\033[0m: %v", m.ListenPort, info.ContainerName, err)
    continue
}
if info.TLS {
    if s.tlsConfig == nil {
        log.Printf("proxy: TLS requested for \033[33m%s\033[0m port %d but TLS config unavailable; falling back to plain TCP",
            info.ContainerName, m.ListenPort)
    } else {
        ln = tls.NewListener(ln, s.tlsConfig)
        log.Printf("proxy: TLS enabled for \033[33m%s\033[0m port %d", info.ContainerName, m.ListenPort)
    }
}
```

### Cert generation

```go
func GenerateSelfSignedTLSConfig() (*tls.Config, error) {
    key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    if err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    now := time.Now()
    tmpl := x509.Certificate{
        SerialNumber:          big.NewInt(1),
        Subject:               pkix.Name{CommonName: "lazy-tcp-proxy"},
        NotBefore:             now.Add(-time.Minute),
        NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
        KeyUsage:              x509.KeyUsageDigitalSignature,
        ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        BasicConstraintsValid: true,
    }
    certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
    if err != nil {
        return nil, fmt.Errorf("create certificate: %w", err)
    }
    cert := tls.Certificate{
        Certificate: [][]byte{certDER},
        PrivateKey:  key,
    }
    log.Printf("proxy: TLS self-signed certificate generated, expires %s",
        now.Add(10*365*24*time.Hour).Format("2006-01-02"))
    return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
```

---

## Risks & Open Questions

- **HTTP/1.0 and non-HTTP connections with `api_key` set**: a non-HTTP TCP connection (e.g. raw binary protocol) will cause `http.ReadRequest` to return an error, and the connection will be dropped silently (no 401, since we can't form one). This is acceptable per the design doc.
- **`bufio.Reader` buffering and TLS**: when `tls: true` is set on a listener, `conn` in `handleConn` is already a `*tls.Conn`. Wrapping it in `bufio.Reader` works fine — `bufio.Reader` reads from whatever `io.Reader` it wraps.
- **`req.Write` vs `req.WriteProxy`**: `req.Write` writes the request in HTTP/1.1 format suitable for sending to an origin server (not a proxy), which is what we want. `req.WriteProxy` would include the full URL in the request line, which is only needed for explicit HTTP proxies.
- **Streaming / large bodies**: `resp.Write` streams the body from `ubr` directly to `client`. For large files or chunked responses this works without buffering the entire body in memory.
