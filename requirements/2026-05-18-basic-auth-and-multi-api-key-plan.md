# Basic Auth Support and Multi-Value API Key — Implementation Plan

**Requirement**: [2026-05-18-basic-auth-and-multi-api-key.md](2026-05-18-basic-auth-and-multi-api-key.md)
**Date**: 2026-05-18
**Status**: Draft

## Implementation Steps

1. **`internal/types/types.go`** — change `APIKey string` → `APIKey []string`; add `BasicAuth []string` field to `TargetInfo`. Add `ParseAuthList` helper (comma-split, trim, skip blank).

2. **`internal/config/store.go`** — change `ServiceEntry.APIKey` from `string` to `[]string`; add `ServiceEntry.BasicAuth []string`; update `entryToTargetInfo` to assign both slices directly (no join/parse); update the placeholder YAML comment block to show the new array form for `api_key` and the new `basic_auth` field.

3. **`internal/proxy/server.go`**:
   - Change `targetState.apiKey string` → `apiKey []string`; add `basicAuth []string`.
   - In `RegisterTarget` (both new-target and update-existing paths): assign `info.APIKey` and `info.BasicAuth`.
   - In `handleConn`: change the HTTP-mode gate from `ts.apiKey != ""` to `len(ts.apiKey) > 0 || len(ts.basicAuth) > 0`.
   - In `handleHTTPProxy`: replace the current single-string API-key equality check with:
     1. **Basic Auth check** (if `len(ts.basicAuth) > 0`): parse `Authorization: Basic <b64>`, decode, check against each entry using `subtle.ConstantTimeCompare`; reject 401 with `WWW-Authenticate` if no match.
     2. **API key check** (if `len(ts.apiKey) > 0`): check `X-API-Key` header against each entry using `subtle.ConstantTimeCompare`; reject 401 if no match.
     3. Strip both `Authorization` and `X-API-Key` headers before forwarding (only the ones that were checked).
   - In `targetInfoEqual`: replace `a.APIKey == b.APIKey` with `reflect.DeepEqual(a.APIKey, b.APIKey)`; add `reflect.DeepEqual(a.BasicAuth, b.BasicAuth)`.

4. **`internal/docker/manager.go`** — replace single `strings.TrimSpace(labels["lazy-tcp-proxy.api-key"])` with `types.ParseAuthList("lazy-tcp-proxy.api-key", labels["lazy-tcp-proxy.api-key"])`; add `BasicAuth: types.ParseAuthList("lazy-tcp-proxy.basic-auth", labels["lazy-tcp-proxy.basic-auth"])`.

5. **`internal/k8s/backend.go`** — same changes as docker manager.

6. **`internal/proxy/server_test.go`** — update all existing `apiKey: "secret"` literals to `apiKey: []string{"secret"}`; add tests:
   - `TestHandleHTTPProxy_MultipleAPIKeys_AnyMatch`
   - `TestHandleHTTPProxy_BasicAuth_CorrectCredentials`
   - `TestHandleHTTPProxy_BasicAuth_MissingHeader`
   - `TestHandleHTTPProxy_BasicAuth_WrongCredentials`
   - `TestHandleHTTPProxy_BasicAuth_MultipleCredentials_AnyMatch`
   - `TestHandleHTTPProxy_BasicAuth_HeaderStripped`
   - `TestTargetInfoEqual_BasicAuthDiffers`
   - `TestTargetInfoEqual_APIKeySliceDiffers`

7. **`README_LABELS.md`** — update the `lazy-tcp-proxy.api-key` row to describe comma-separated multi-key format; add a new `lazy-tcp-proxy.basic-auth` row; add/expand the auth section with Basic Auth examples.

8. **`README_CONFIG.md`** — update the `api_key` example from `api_key: "key"` to `api_key: ["key"]`; add `basic_auth` example.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/types/types.go` | Modify | `APIKey []string`, add `BasicAuth []string`, add `ParseAuthList` |
| `lazy-tcp-proxy/internal/config/store.go` | Modify | `ServiceEntry.APIKey []string`, add `BasicAuth []string`, update placeholder |
| `lazy-tcp-proxy/internal/proxy/server.go` | Modify | `targetState` fields, HTTP-mode gate, `handleHTTPProxy`, `targetInfoEqual` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Parse `api-key` and `basic-auth` labels using `ParseAuthList` |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Same as docker manager |
| `lazy-tcp-proxy/internal/proxy/server_test.go` | Modify | Update existing tests; add new Basic Auth and multi-key tests |
| `README_LABELS.md` | Modify | Document `lazy-tcp-proxy.basic-auth`; update `api-key` multi-value description |
| `README_CONFIG.md` | Modify | Update `api_key` example to array form; add `basic_auth` example |

## Key Code Snippets

### `ParseAuthList` (types/types.go)

```go
// ParseAuthList splits a comma-separated label/annotation value into a slice
// of trimmed, non-empty strings. Used for api-key and basic-auth lists.
func ParseAuthList(label, s string) []string {
    var out []string
    for _, token := range strings.Split(s, ",") {
        v := strings.TrimSpace(token)
        if v != "" {
            out = append(out, v)
        }
    }
    return out
}
```

### `handleHTTPProxy` auth checks (proxy/server.go)

```go
// Basic Auth check
if len(ts.basicAuth) > 0 {
    authHeader := req.Header.Get("Authorization")
    const prefix = "Basic "
    ok := false
    if strings.HasPrefix(authHeader, prefix) {
        decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
        if err == nil {
            for _, cred := range ts.basicAuth {
                if subtle.ConstantTimeCompare(decoded, []byte(cred)) == 1 {
                    ok = true
                    break
                }
            }
        }
    }
    if !ok {
        log.Printf("proxy: basic-auth: rejected request to %s from %s", ts.info.ContainerName, client.RemoteAddr())
        client.Write([]byte(
            "HTTP/1.1 401 Unauthorized\r\n" +
            "WWW-Authenticate: Basic realm=\"lazy-tcp-proxy\"\r\n" +
            "Content-Length: 0\r\n" +
            "Connection: close\r\n\r\n"))
        return
    }
    req.Header.Del("Authorization")
}

// API key check
if len(ts.apiKey) > 0 {
    got := req.Header.Get("X-API-Key")
    ok := false
    for _, k := range ts.apiKey {
        if subtle.ConstantTimeCompare([]byte(got), []byte(k)) == 1 {
            ok = true
            break
        }
    }
    if !ok {
        log.Printf("proxy: api-key: rejected request to %s from %s", ts.info.ContainerName, client.RemoteAddr())
        client.Write([]byte(
            "HTTP/1.1 401 Unauthorized\r\n" +
            "Content-Length: 0\r\n" +
            "Connection: close\r\n\r\n"))
        return
    }
    req.Header.Del("X-API-Key")
}
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestHandleHTTPProxy_CorrectKey` (updated) | `apiKey: []string{"secret"}`, `X-API-Key: secret` | 200 OK |
| `TestHandleHTTPProxy_MissingKey` (updated) | `apiKey: []string{"secret"}`, no header | 401 |
| `TestHandleHTTPProxy_WrongKey` (updated) | `apiKey: []string{"secret"}`, `X-API-Key: wrong` | 401 |
| `TestHandleHTTPProxy_MultipleAPIKeys_AnyMatch` | `apiKey: []string{"key1","key2"}`, `X-API-Key: key2` | 200 OK |
| `TestHandleHTTPProxy_BasicAuth_CorrectCredentials` | `basicAuth: []string{"nick:pass"}`, `Authorization: Basic bmlja3Bhc3M=` | 200 OK |
| `TestHandleHTTPProxy_BasicAuth_MissingHeader` | `basicAuth: []string{"nick:pass"}`, no header | 401 with `WWW-Authenticate` |
| `TestHandleHTTPProxy_BasicAuth_WrongCredentials` | `basicAuth: []string{"nick:pass"}`, wrong decoded value | 401 |
| `TestHandleHTTPProxy_BasicAuth_MultipleCredentials_AnyMatch` | `basicAuth: []string{"nick:pass","alice:other"}`, `Authorization: Basic YWxpY2U6b3RoZXI=` | 200 OK |
| `TestHandleHTTPProxy_BasicAuth_HeaderStripped` | correct basic auth | upstream receives no `Authorization` header |
| `TestTargetInfoEqual_BasicAuthDiffers` | two infos with different `BasicAuth` slices | not equal |
| `TestTargetInfoEqual_APIKeySliceDiffers` | two infos with different `APIKey` slices | not equal |

## Risks & Open Questions

- The `encoding/base64` standard encoding is used; some clients send URL-safe or raw base64. Standard encoding covers all major HTTP clients (browsers, curl) so this is acceptable.
- If a service has both `basic_auth` and `api_key`, both checks run sequentially — reject on the first failure. This is the least surprising behaviour.
