# Container Availability Config — Implementation Plan

**Requirement**: [2026-05-27-container-availability-config.md](2026-05-27-container-availability-config.md)
**Date**: 2026-05-27
**Status**: Implemented

## Implementation Steps

1. **`internal/types/types.go`** — add the `Availability` field, string constants, a
   parse helper, and the `EffectiveAvailability` function.

2. **`internal/docker/manager.go`** — parse `lazy-tcp-proxy.availability` in
   `containerToTargetInfo` and populate `TargetInfo.Availability`.

3. **`internal/k8s/backend.go`** — parse `lazy-tcp-proxy.availability` annotation
   and populate `TargetInfo.Availability`.

4. **`internal/config/store.go`** — add `Availability string` to `ServiceEntry`,
   pass it through in `entryToTargetInfo`, add validation, and update the
   placeholder comment.

5. **`internal/proxy/server.go`** — four sub-changes:
   a. `checkInactivity`: replace the `CronStart/CronStop` exemption check with
      `types.EffectiveAvailability(info) != types.AvailabilityOnDemand`.
   b. `handleConn`: guard the `EnsureRunning` block so it is skipped when
      `ts.info.Availability` is `"cron"` or `"manual"` (explicit only —
      derived-cron preserves the existing behaviour).
   c. `RegisterTarget`: gate cron scheduler registration on
      `types.EffectiveAvailability(info) == types.AvailabilityCron`.
   d. `targetInfoEqual`: add `a.Availability == b.Availability`.

6. **`internal/proxy/udp.go`** — guard the `EnsureRunning` block in
   `startUDPFlow` with the same explicit-availability check as step 5b.

7. **`internal/proxy/server_test.go`** — add unit tests covering:
   - Inactivity checker skips explicit `cron` and `manual` availability.
   - Inactivity checker skips derived-cron (CronStart/CronStop set, no explicit
     availability).
   - Inactivity checker applies when `availability: ondemand` is explicit.

8. **`README_LABELS.md`** — add `lazy-tcp-proxy.availability` row to the label
   reference table; add a short explanatory paragraph near the Cron Scheduling
   section.

9. **`README_CONFIG.md`** — add `availability` field to the YAML schema example
   and field reference table.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modify | Add `Availability` field, constants, `ParseAvailabilityLabel`, `EffectiveAvailability` |
| `internal/docker/manager.go` | Modify | Parse `lazy-tcp-proxy.availability` label |
| `internal/k8s/backend.go` | Modify | Parse `lazy-tcp-proxy.availability` annotation |
| `internal/config/store.go` | Modify | Add `Availability` to `ServiceEntry`, validate it, pass to `TargetInfo`, update placeholder |
| `internal/proxy/server.go` | Modify | `checkInactivity`, `handleConn`, `RegisterTarget`, `targetInfoEqual` |
| `internal/proxy/udp.go` | Modify | `startUDPFlow` EnsureRunning guard |
| `internal/proxy/server_test.go` | Modify | New inactivity-checker tests for each availability mode |
| `README_LABELS.md` | Modify | Add `availability` label row and explanation |
| `README_CONFIG.md` | Modify | Add `availability` YAML field |

---

## Key Code Snippets

### `types.go` — new constants and helpers

```go
const (
    AvailabilityOnDemand = "ondemand"
    AvailabilityCron     = "cron"
    AvailabilityManual   = "manual"
)

// EffectiveAvailability resolves the active lifecycle management mode.
// If info.Availability is set explicitly, that value is returned.
// Otherwise it is derived: "cron" if either cron expression is set, else "ondemand".
func EffectiveAvailability(info TargetInfo) string {
    if info.Availability != "" {
        return info.Availability
    }
    if info.CronStart != "" || info.CronStop != "" {
        return AvailabilityCron
    }
    return AvailabilityOnDemand
}

// ParseAvailabilityLabel validates the availability label/annotation value.
// Returns "" (derive from context) if the value is absent or empty.
// Logs a warning and returns "" for unrecognised values.
func ParseAvailabilityLabel(name, raw string) string {
    v := strings.TrimSpace(raw)
    switch v {
    case "", AvailabilityOnDemand, AvailabilityCron, AvailabilityManual:
        return v
    default:
        log.Printf("container %s: ignoring invalid availability %q (must be ondemand, cron, or manual)", name, v)
        return ""
    }
}
```

### `proxy/server.go` — `checkInactivity` before (TCP excerpt)

```go
if ts.info.CronStart != "" || ts.info.CronStop != "" {
    continue // lifecycle managed by cron scheduler
}
```

After:

```go
if types.EffectiveAvailability(ts.info) != types.AvailabilityOnDemand {
    continue // lifecycle not managed on-demand
}
```

(Same replacement for the UDP loop in `checkInactivity`.)

### `proxy/server.go` — `handleConn` EnsureRunning guard

```go
// Wrap the existing EnsureRunning block:
if ts.info.Availability != types.AvailabilityCron && ts.info.Availability != types.AvailabilityManual {
    _, startErr, shared := s.startGroup.Do(ts.info.ContainerID, func() (any, error) {
        return nil, s.backend.EnsureRunning(ctx, ts.info.ContainerID)
    })
    if shared {
        log.Printf("proxy: joined in-flight startup for \033[33m%s\033[0m", ts.info.ContainerName)
    }
    if startErr != nil {
        log.Printf("proxy: could not start container \033[33m%s\033[0m: %v", ts.info.ContainerName, startErr)
        return
    }
    s.mu.Lock()
    for _, t := range s.targets {
        if t.info.ContainerID == ts.info.ContainerID {
            t.running = true
        }
    }
    for _, u := range s.udpTargets {
        if u.info.ContainerID == ts.info.ContainerID {
            u.running = true
        }
    }
    s.mu.Unlock()
    if ts.info.WebhookURL != "" {
        go s.fireWebhook(ts.info.WebhookURL, "container_started", ...)
    }
}
```

### `proxy/server.go` — `RegisterTarget` cron scheduler gate

```go
// Before:
if s.sched != nil && (info.CronStart != "" || info.CronStop != "") {
    s.sched.Register(info)
}

// After:
if s.sched != nil &&
    types.EffectiveAvailability(info) == types.AvailabilityCron &&
    (info.CronStart != "" || info.CronStop != "") {
    s.sched.Register(info)
}
```

### `proxy/udp.go` — `startUDPFlow` EnsureRunning guard

```go
if uls.info.Availability != types.AvailabilityCron && uls.info.Availability != types.AvailabilityManual {
    _, startErr, shared := s.startGroup.Do(uls.info.ContainerID, func() (any, error) {
        return nil, s.backend.EnsureRunning(ctx, uls.info.ContainerID)
    })
    if shared { ... }
    if startErr != nil { cleanup(); return }
}
```

---

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestCheckInactivity_SkipsExplicitCronAvailability` | target with `Availability:"cron"`, idle | StopContainer NOT called |
| `TestCheckInactivity_SkipsManualAvailability` | target with `Availability:"manual"`, idle | StopContainer NOT called |
| `TestCheckInactivity_SkipsDerivedCron` | target with `CronStart:"0 9 * * 1-5"`, no explicit availability, idle | StopContainer NOT called |
| `TestCheckInactivity_AppliesExplicitOnDemand` | target with `Availability:"ondemand"`, idle | StopContainer called |
| `TestParseAvailabilityLabel_Valid` | `"ondemand"`, `"cron"`, `"manual"`, `""` | returned as-is |
| `TestParseAvailabilityLabel_Invalid` | `"always"` | returns `""`, warning logged |
| `TestEffectiveAvailability` | various combos of Availability + CronStart/Stop | correct string |

---

## Risks & Open Questions

- **UDP `startUDPFlow`**: The `EnsureRunning` block also sets `uls.running = true` and
  manages `upstreamStarting` state. When skipping `EnsureRunning`, we do NOT set
  `uls.running = true` (correct — the container might not be running). The retry
  loop for upstream readiness runs regardless, so the UDP flow will either succeed
  (if the container is up) or time out (if not). This is acceptable for `cron` /
  `manual` modes.

- **Webhook `container_started` event**: Currently fired immediately after a
  successful `EnsureRunning`. When `EnsureRunning` is skipped (explicit `cron`/
  `manual`), the webhook is not fired on connection. This is correct — the container
  wasn't started by the proxy.

- **`ondemand` + cron expressions**: No warning is logged in the current design;
  the cron expressions are simply ignored. If we decide a warning is valuable, it
  can be added in `RegisterTarget` before the scheduler gate — out of scope for
  this iteration.
