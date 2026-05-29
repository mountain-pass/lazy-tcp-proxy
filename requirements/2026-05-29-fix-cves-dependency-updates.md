# Fix: CVE Dependency Updates (REQ-091)

**Date Added**: 2026-05-29
**Priority**: High
**Status**: Completed

## Problem Statement

New compose/docker dependencies introduced in REQ-088 pulled in older transitive
dependency versions that carry high and critical severity CVEs.  10 CVEs were
reported affecting four packages.

## CVEs Fixed

| CVE | Severity | Package | Old Version | New Version |
|-----|----------|---------|-------------|-------------|
| CVE-2026-46595 | 10.0 (C) | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-42508 | 9.1 (C)  | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-39831 | 9.1 (C)  | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-39830 | 9.1 (C)  | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-39832 | 9.1 (C)  | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-39833 | 9.1 (C)  | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-39834 | 9.1 (C)  | golang.org/x/crypto | 0.42.0 | 0.52.0 |
| CVE-2026-39821 | 9.6 (C)  | golang.org/x/net    | 0.45.0 | 0.55.0 |
| CVE-2026-33186 | 9.1 (C)  | google.golang.org/grpc | 1.74.2 | 1.81.1 |
| CVE-2026-34040 | 8.8 (H)  | github.com/docker/docker | 28.5.1 | 28.5.2 |

## Changes

- `golang.org/x/crypto`: 0.42.0 → 0.52.0
- `golang.org/x/net`: 0.45.0 → 0.55.0
- `google.golang.org/grpc`: 1.74.2 → 1.81.1
- `github.com/docker/docker`: 28.5.1 → 28.5.2

Transitive dependencies also updated as part of `go mod tidy`.

## Acceptance Criteria

- [x] All four packages updated to versions beyond the vulnerable range.
- [x] `go build ./...` passes.
- [x] `golangci-lint run ./...` passes with 0 issues.
