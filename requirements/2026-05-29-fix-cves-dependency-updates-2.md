# Fix: CVE Dependency Updates — Batch 2 (REQ-092)

**Date Added**: 2026-05-29
**Priority**: High
**Status**: Completed

## Problem Statement

Pages 2–4 of the Dependabot security advisory report identified 30 additional
CVEs across six packages, all introduced as transitive dependencies of the
compose/docker libraries added in REQ-088.

## CVEs Fixed

| CVE | Severity | Package | Old Version | New Version |
|-----|----------|---------|-------------|-------------|
| CVE-2026-35469 | 8.7 (H) | github.com/moby/spdystream | 0.5.0 | 0.5.1 |
| CVE-2026-33747 | 8.4 (H) | github.com/moby/buildkit | 0.25.1 | 0.26.3 |
| CVE-2026-33748 | 8.2 (H) | github.com/moby/buildkit | 0.25.1 | 0.26.3 |
| CVE-2025-47913 | 7.5 (H) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-46597 | 7.5 (H) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-39829 | 7.5 (H) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-33814 | 7.5 (H) | golang.org/x/net | 0.45.0 | 0.55.0 (REQ-091) |
| CVE-2024-25621 | 7.3 (H) | github.com/containerd/containerd/v2 | 2.1.4 | 2.2.4 |
| CVE-2026-46680 | 7.3 (H) | github.com/containerd/containerd/v2 | 2.1.4 | 2.2.4 |
| CVE-2026-39883 | 7.3 (H) | go.opentelemetry.io/otel/sdk | 1.41.0 | 1.44.0 |
| CVE-2026-41567 | 7.2 (H) | github.com/docker/docker | 28.5.1 | 28.5.2 (REQ-091) |
| CVE-2026-42306 | 7.2 (H) | github.com/docker/docker | 28.5.1 | 28.5.2 (REQ-091) |
| CVE-2025-15558 | 7.0 (H) | github.com/docker/cli | 28.5.1 | 28.5.2 |
| CVE-2025-64329 | 6.9 (M) | github.com/containerd/containerd/v2 | 2.1.4 | 2.2.4 |
| CVE-2026-33997 | 6.8 (M) | github.com/docker/docker | 28.5.1 | 28.5.2 (REQ-091) |
| CVE-2026-39827 | 6.5 (M) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-25680 | 6.5 (M) | golang.org/x/net | 0.45.0 | 0.55.0 (REQ-091) |
| CVE-2026-39828 | 6.3 (M) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-25681 | 6.1 (M) | golang.org/x/net | 0.45.0 | 0.55.0 (REQ-091) |
| CVE-2026-42506 | 6.1 (M) | golang.org/x/net | 0.45.0 | 0.55.0 (REQ-091) |
| CVE-2026-27136 | 6.1 (M) | golang.org/x/net | 0.45.0 | 0.55.0 (REQ-091) |
| CVE-2026-42502 | 6.1 (M) | golang.org/x/net | 0.45.0 | 0.55.0 (REQ-091) |
| CVE-2026-41568 | 6.1 (M) | github.com/docker/docker | 28.5.1 | 28.5.2 (REQ-091) |
| CVE-2026-46598 | 5.3 (M) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-39835 | 5.3 (M) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2025-47914 | 5.3 (M) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2025-58181 | 5.3 (M) | golang.org/x/crypto | 0.42.0 | 0.52.0 (REQ-091) |
| CVE-2026-39882 | 5.3 (M) | go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp | 1.35.0 | 1.44.0 |
| GHSA-pmwq-pjrm-6p5r | 4.1 (M) | github.com/in-toto/in-toto-golang | 0.9.0 | 0.11.0 |
| CVE-2026-39824 | 3.3 (L) | golang.org/x/sys | 0.41.0 | 0.45.0 (REQ-091) |

## Changes in This PR

- `github.com/moby/spdystream`: 0.5.0 → 0.5.1
- `github.com/moby/buildkit`: 0.25.1 → 0.26.3
- `github.com/containerd/containerd/v2`: 2.1.4 → 2.2.4
- `github.com/docker/cli`: 28.5.1 → 28.5.2
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace*`: 1.35.0 → 1.44.0
- `github.com/in-toto/in-toto-golang`: 0.9.0 → 0.11.0

Note: x/crypto, x/net, docker/docker, otel/sdk, and x/sys were already updated in REQ-091.

Note on `moby/buildkit`: v0.26.3 is the highest patch release that retains the
`util/tracing/env` package required by `docker/compose/v2 v2.40.3`. Versions ≥
v0.30.0 removed that package, breaking the build.

## Acceptance Criteria

- [x] All six packages updated to versions beyond the vulnerable range.
- [x] `go build ./...` passes.
- [x] `golangci-lint run ./...` passes with 0 issues.
