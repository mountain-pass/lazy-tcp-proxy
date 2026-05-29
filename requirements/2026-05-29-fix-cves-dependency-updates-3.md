# Fix: CVE Dependency Updates — Batch 3 (REQ-093)

**Date Added**: 2026-05-29
**Priority**: High
**Status**: Completed

## Problem Statement

After deploying REQ-092, 9 CVEs remained. This requirement addresses the ones
that are fixable given current dependency constraints.

## CVEs Fixed

| CVE | Severity | Package | Old Version | New Version |
|-----|----------|---------|-------------|-------------|
| CVE-2026-39882 | 5.3 (M) | go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp | 1.38.0 | 1.44.0 |

## CVEs Not Fixable (Blocked by Upstream Constraints)

The following 8 CVEs cannot be fixed at this time due to a transitive dependency
conflict between `docker/compose/v2 v2.40.3` and `moby/moby/client`:

| CVE | Severity | Package | Issue |
|-----|----------|---------|-------|
| CVE-2026-34040  | 8.8 (H) | github.com/docker/docker | No v29.x available upstream; v28.5.2 is latest |
| CVE-2026-33747  | 8.4 (H) | github.com/moby/buildkit | v0.27+ triggers docker/buildx type conflict |
| CVE-2026-33748  | 8.2 (H) | github.com/moby/buildkit | v0.27+ triggers docker/buildx type conflict |
| CVE-2026-41567  | 7.2 (H) | github.com/docker/docker | No v29.x available upstream |
| CVE-2026-42306  | 7.2 (H) | github.com/docker/docker | No v29.x available upstream |
| CVE-2025-15558  | 7.0 (H) | github.com/docker/cli | v29.x imports docker/buildx types; type conflict |
| CVE-2026-33997  | 6.8 (M) | github.com/docker/docker | No v29.x available upstream |
| CVE-2026-41568  | 6.1 (M) | github.com/docker/docker | No v29.x available upstream |

### Root Cause of Blocked CVEs

`docker/compose/v2 v2.40.3` depends on `docker/buildx@v0.29.1`, which uses
`github.com/docker/docker/client` types. Our codebase uses the `moby/moby/client`
module (a separate Go module). When `docker/cli ≥ v29.x` or `moby/buildkit ≥ v0.27`
is used, the `docker/buildx` packages that mix both type namespaces get compiled,
causing a build failure.

The fix requires either:
- A new release of `docker/compose/v2` that depends on a `docker/buildx` version
  using only `moby/moby` types, or
- Replacing the Go compose library with direct invocation of the `docker compose`
  CLI binary.

## Changes in This PR

- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp`: 1.38.0 → 1.44.0
- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`: 1.38.0 → 1.44.0

## Acceptance Criteria

- [x] otel metric exporter updated to v1.44.0.
- [x] `go build ./...` passes.
- [x] `golangci-lint run ./...` passes with 0 issues.
