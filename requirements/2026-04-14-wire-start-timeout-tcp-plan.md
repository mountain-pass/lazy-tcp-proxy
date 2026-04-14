# Wire START_TIMEOUT_SECS to TCP Dial Retry Loop — Implementation Plan

**Requirement**: [2026-04-14-wire-start-timeout-tcp.md](2026-04-14-wire-start-timeout-tcp.md)
**Date**: 2026-04-14
**Status**: Draft

## Implementation Steps

1. **`internal/proxy/server.go`** — Remove the `dialRetries` constant; add `startTimeout time.Duration` to `targetState`; populate it in `RegisterTarget` (new and update paths); replace `dialRetries` with a runtime-derived `retries` value in `handleConn`.

That is the only file that changes.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/proxy/server.go` | Modify | Remove `dialRetries`; add `targetState.startTimeout`; derive retries in `handleConn` |

## Key Code Snippets

### Constants block (`server.go:26`)

Remove `dialRetries`; keep `dialInterval`:

```go
const (
    dialInterval = time.Second
    copyBufSize  = 32 * 1024
)
```

### `targetState` struct

```go
type targetState struct {
    ...
    idleTimeout  *time.Duration
    startTimeout time.Duration  // add this
    ...
}
```

### `RegisterTarget` — new TCP listener path

```go
ts := &targetState{
    ...
    idleTimeout:  info.IdleTimeout,
    startTimeout: effectiveTimeout(info.StartTimeout, s.startTimeout),
    ...
}
```

### `RegisterTarget` — update path (existing TCP listener)

```go
existing.idleTimeout  = info.IdleTimeout
existing.startTimeout = effectiveTimeout(info.StartTimeout, s.startTimeout)  // add
```

### `handleConn` — dial retry loop

Replace the hardcoded `dialRetries` with:

```go
retries := int((ts.startTimeout + dialInterval - 1) / dialInterval)
if retries < 1 {
    retries = 1
}
for attempt := 1; attempt <= retries; attempt++ {
    ...
}
```

## Unit Tests

No new tests required — the derivation formula is identical to the one already
tested indirectly by the UDP integration tests. The existing TCP integration
test (`TestTCPProxy_ForwardsData`) exercises the dial loop with `startTimeout =
30s` (set in `newIntegrationServer`), confirming the path compiles and works.

## Risks & Open Questions

None — this is a pure wire-up of an already-existing field through one
additional code path.
