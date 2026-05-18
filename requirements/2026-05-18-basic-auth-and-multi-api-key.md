# Basic Auth Support and Multi-Value API Key

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Completed

## Problem Statement

Some clients cannot set custom HTTP headers (e.g. `X-API-Key`) but can embed credentials in the request URL (`https://nick:somepassword@myservice.com`). This causes the HTTP client to send an `Authorization: Basic <base64>` header. The proxy currently has no support for Basic Auth, so these clients cannot be authenticated.

Additionally, the current `api_key` configuration accepts only a single value, which makes key rotation (providing two valid keys during a transition window) impossible.

## Functional Requirements

### Basic Auth (`basic_auth` / `lazy-tcp-proxy.basic-auth`)

- When `basic_auth` is set on a service, every inbound HTTP request must carry a valid `Authorization: Basic <credentials>` header.
- The proxy decodes the Base64 credentials and checks `user:password` against the configured list. If **any** entry matches, the request is allowed.
- If the header is absent or no entry matches, the proxy returns `HTTP 401 Unauthorized` with `WWW-Authenticate: Basic realm="lazy-tcp-proxy"` and closes the connection.
- On success, the `Authorization` header is stripped before forwarding to the upstream container.
- `basic_auth` is independent of `tls` and `api_key` — each can be used alone or in combination.
- Multiple credentials allow concurrent key rotation: `["nick:oldpass", "nick:newpass"]`.

### Multi-Value API Key (`api_key`)

- `api_key` is changed from a single string to a list of strings. Only the array form is supported going forward; single-string YAML is no longer valid.
- If **any** value in the list matches the `X-API-Key` header, the request is allowed.
- Docker/Kubernetes label format: comma-separated values, e.g. `lazy-tcp-proxy.api-key=key1,key2`.

### Combined usage

- `api_key` and `basic_auth` can coexist on the same service; **both** checks apply — a request must satisfy whichever auth method(s) are configured.
- Actually: if both are set, the proxy applies whichever one(s) are configured independently. In practice the user would configure one or the other, not both, but the implementation must handle the combination gracefully (apply both checks; reject if either fails).

## User Experience Requirements

### YAML config

```yaml
services:
  - name: "my-container"
    ports:
      - "9000:80"
    basic_auth:
      - "nick:somepassword"
      - "alice:otherpassword"
    api_key:
      - "somekey1"
      - "somekey2"
```

### Docker labels

```yaml
labels:
  - "lazy-tcp-proxy.basic-auth=nick:somepassword,alice:otherpassword"
  - "lazy-tcp-proxy.api-key=somekey1,somekey2"
```

### Kubernetes annotations

```yaml
annotations:
  lazy-tcp-proxy.basic-auth: "nick:somepassword,alice:otherpassword"
  lazy-tcp-proxy.api-key: "somekey1,somekey2"
```

## Technical Requirements

- `types.TargetInfo.APIKey` changes from `string` to `[]string`.
- A new `types.TargetInfo.BasicAuth` field is added as `[]string` (each entry is `user:password`).
- YAML `ServiceEntry.APIKey` is `[]string`; only the array form is accepted.
- YAML `ServiceEntry.BasicAuth` is `[]string`.
- Docker/K8s label parsing: `lazy-tcp-proxy.api-key` is split on commas; `lazy-tcp-proxy.basic-auth` is split on commas.
- `handleHTTPProxy` checks `basic_auth` first (if set), then `api_key` (if set). Each is independently evaluated; rejection on either failure.
- `targetInfoEqual` is updated to include `BasicAuth` and the slice-equality for `APIKey`.
- The config placeholder in `store.go` is updated to show both options.

## Acceptance Criteria

- [x] `basic_auth: ["nick:somepassword"]` in YAML causes the proxy to require valid Basic Auth credentials.
- [x] A request without `Authorization` header returns `401 Unauthorized`.
- [x] A request with wrong credentials returns `401 Unauthorized`.
- [x] A request with any of the listed credentials is forwarded (header stripped).
- [x] `lazy-tcp-proxy.basic-auth=nick:pass` Docker label behaves identically to the YAML setting.
- [x] `api_key: ["key1", "key2"]` YAML accepts requests with either key.
- [x] `lazy-tcp-proxy.api-key=key1,key2` Docker label accepts requests with either key.
- [x] `targetInfoEqual` returns false when `APIKey` lists differ.
- [x] `targetInfoEqual` returns false when `BasicAuth` lists differ.
- [x] Existing services with neither `api_key` nor `basic_auth` are unaffected (pure TCP passthrough).
- [x] Tests cover: missing auth, wrong auth, correct auth, multiple entries (any-match).

## Dependencies

- REQ-067 (Per-Service TLS Termination and API Key Authentication) — modifies the `APIKey` field and `handleHTTPProxy` function introduced by that requirement.
- REQ-065 (Dynamic Configuration File) — adds fields to `ServiceEntry`.

## Implementation Notes

- Basic Auth check: `encoding/base64` decode the value after stripping the `Basic ` prefix from the `Authorization` header. Compare decoded `user:password` against each entry in `BasicAuth`. Constant-time comparison (`subtle.ConstantTimeCompare`) should be used to avoid timing attacks.
- API key check similarly uses constant-time comparison for each candidate key.
- `WWW-Authenticate: Basic realm="lazy-tcp-proxy"` is included in 401 responses for Basic Auth so browsers show a login dialog.
