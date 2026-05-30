# Basic Auth: Replace Cleartext Passwords with htpasswd Bcrypt Hashes

**Date Added**: 2026-05-30
**Priority**: High
**Status**: Completed

## Problem Statement

`basic_auth` entries were stored and compared as cleartext `user:password` strings. Storing plaintext passwords in configuration files, Docker labels, and Kubernetes annotations is a security risk — if the config is exposed, all credentials are immediately compromised.

## Functional Requirements

- `basic_auth` entries must use the **htpasswd bcrypt format**: `user:$2y$...` (or `$2a$...`).
- The proxy validates incoming credentials by:
  1. Comparing the submitted username against the stored username (constant-time).
  2. Verifying the submitted password against the stored bcrypt hash using `bcrypt.CompareHashAndPassword`.
- Cleartext passwords are no longer accepted.
- Users generate entries with `htpasswd -nbB <user> <password>`.

## Technical Requirements

- `internal/proxy/server.go`: Import `golang.org/x/crypto/bcrypt`; replace `subtle.ConstantTimeCompare` on the full decoded credential with username constant-time compare + `bcrypt.CompareHashAndPassword` on the password portion.
- `golang.org/x/crypto` promoted from indirect to direct dependency in `go.mod`.
- Tests updated to generate bcrypt hashes at test time via `bcrypt.GenerateFromPassword`.
- Config placeholder, `README_CONFIG.md`, and `README_LABELS.md` updated to document the htpasswd format and how to generate hashes.

## Acceptance Criteria

- [x] Correct bcrypt-hashed credentials are accepted.
- [x] Wrong password (bcrypt mismatch) returns 401.
- [x] Missing Authorization header returns 401.
- [x] Multiple entries: any matching `user:hash` pair is accepted.
- [x] Authorization header is stripped before forwarding on success.
- [x] All existing `TestHandleHTTPProxy_BasicAuth_*` tests pass with hashed credentials.
- [x] Documentation updated in `README_CONFIG.md` and `README_LABELS.md`.

## Dependencies

- REQ-078 (Basic Auth Support and Multi-Value API Key) — introduced the `BasicAuth` field and validation logic this requirement updates.
