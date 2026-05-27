# Container Availability Config (`availability`)

**Date Added**: 2026-05-27
**Priority**: Medium
**Status**: In Progress

## Problem Statement

The proxy currently derives container lifecycle management behaviour implicitly: if
`cron-start` or `cron-stop` labels are set the container is treated as
cron-managed (idle timeout disabled); otherwise it is treated as on-demand
(started on first connection, stopped when idle). There is no way to explicitly
declare that a container should be treated as a pure passthrough with no
lifecycle management at all.

Users running containers that are managed entirely by an external scheduler
(e.g. a host cron job, CI runner, or another orchestration tool) have no way to
tell the proxy "leave this container alone; just forward traffic". Without this,
the proxy may attempt to start or stop containers it should never touch.

## Functional Requirements

1. A new optional config field `availability` is added, accepted in:
   - Docker label: `lazy-tcp-proxy.availability`
   - Kubernetes annotation: `lazy-tcp-proxy.availability`
   - YAML config field: `availability`

2. Valid values:
   - `ondemand` — start on connection, stop when idle (existing on-demand behaviour)
   - `cron` — start/stop via cron schedule; proxy does **not** start the container
     on an incoming connection; idle timeout is disabled
   - `manual` — proxy does **not** start or stop the container for any reason
     (no on-demand, no idle timeout, no cron scheduling); acts as a pure TCP/UDP
     passthrough

3. When `availability` is **not provided**, the existing derived logic applies:
   - If `cron-start` or `cron-stop` is set → effective availability = `cron`
     (backward-compatible: `EnsureRunning` is still called on connection for
     derived-cron, matching today's behaviour)
   - Otherwise → effective availability = `ondemand`

4. When `availability` is explicitly set:
   - `ondemand`: `EnsureRunning` called on connection; idle timeout active;
     cron scheduler **not** used even if cron expressions are present (with a
     logged warning if they are)
   - `cron`: `EnsureRunning` **not** called on connection; idle timeout
     disabled; cron scheduler active if expressions are present
   - `manual`: `EnsureRunning` **not** called; idle timeout disabled; cron
     scheduler **not** used even if cron expressions are present (with a logged
     warning if they are)

5. Invalid values are rejected at label/annotation/YAML parse time with a
   warning log; the target falls back to the derived behaviour.

## User Experience Requirements

**Docker label example:**
```yaml
labels:
  lazy-tcp-proxy.enabled: "true"
  lazy-tcp-proxy.ports: "5432:5432"
  lazy-tcp-proxy.availability: "manual"
```

**Kubernetes annotation example:**
```yaml
annotations:
  lazy-tcp-proxy.enabled: "true"
  lazy-tcp-proxy.ports: "5432:5432"
  lazy-tcp-proxy.availability: "manual"
```

**YAML config example:**
```yaml
services:
  - name: "my-db"
    ports:
      - "5432:5432"
    availability: "manual"
```

## Technical Requirements

- All three backends (Docker label, K8s annotation, YAML config) parse and
  store the value in a new `Availability string` field on `types.TargetInfo`.
- A helper `types.EffectiveAvailability(info TargetInfo) string` computes the
  resolved mode, accounting for derived fallback from cron expressions.
- The inactivity checker skips any target whose effective availability is not
  `"ondemand"`.
- `EnsureRunning` is **not** called for targets with **explicit**
  `Availability == "cron"` or `Availability == "manual"` (derived-cron
  preserves the existing call for backward compatibility).
- The cron scheduler registers a target only when its effective availability
  is `"cron"` (explicit or derived) **and** at least one cron expression is set.
  Targets with `availability: manual` are never registered with the cron
  scheduler.
- `targetInfoEqual` in `proxy/server.go` is updated to include `Availability`.
- The YAML config placeholder comment is updated to show the new field.

## Acceptance Criteria

- [ ] `availability: manual` — proxy forwards connections; `EnsureRunning` is
  never called; idle timeout checker skips the target; cron scheduler not used.
- [ ] `availability: cron` (explicit) — connections are forwarded without
  calling `EnsureRunning`; idle timeout checker skips the target; cron scheduler
  active if cron expressions present.
- [ ] `availability: ondemand` (explicit) — on-demand start and idle timeout
  active; cron expressions present → warning logged, cron scheduler not used.
- [ ] No `availability` label, cron expressions present → backward-compatible
  "derived cron": `EnsureRunning` still called on connection, idle timeout
  skipped, cron scheduler active.
- [ ] No `availability` label, no cron expressions → existing on-demand
  behaviour unchanged.
- [ ] Invalid `availability` value → warning logged, derived behaviour used.
- [ ] All existing unit and integration tests continue to pass.
- [ ] `README_LABELS.md` and `README_CONFIG.md` updated with the new field.

## Dependencies

- Depends on: REQ-001, REQ-048 (cron scheduling), REQ-065 (dynamic config file)
- Affects:
  - `internal/types/types.go`
  - `internal/docker/manager.go`
  - `internal/k8s/backend.go` (kubernetes build)
  - `internal/config/store.go`
  - `internal/proxy/server.go`
  - `internal/proxy/udp.go`
  - `README_LABELS.md`
  - `README_CONFIG.md`

## Implementation Notes

- `types.EffectiveAvailability` returns `"ondemand"`, `"cron"`, or `"manual"`.
  The derived fallback means: if `Availability == ""` and cron expressions are
  set → `"cron"`; if `Availability == ""` and no cron expressions → `"ondemand"`.
- The EnsureRunning skip uses the **explicit** `Availability` field (not
  `EffectiveAvailability`) so that derived-cron containers continue to
  call `EnsureRunning` on connection (backward-compatible).
- The inactivity checker uses `EffectiveAvailability` to decide whether to skip
  a target, which covers both explicit and derived-cron cases uniformly.
