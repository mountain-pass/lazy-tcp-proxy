# Fix UDP ECONNREFUSED Retry and Clarify Internal Flow Log Messages — Implementation Plan

**Requirement**: [2026-04-14-fix-udp-connrefused-retry-and-flow-logs.md](2026-04-14-fix-udp-connrefused-retry-and-flow-logs.md)
**Date**: 2026-04-14
**Status**: Implemented

## Implementation Steps

1. Add `"errors"` and `"syscall"` to the import block in `internal/proxy/udp.go`.
2. In `startUDPFlow`, replace the `if netErr, ok := readErr.(net.Error); ok && netErr.Timeout()`
   branch with a unified `isRetryable` boolean that also returns `true` for
   `errors.Is(readErr, syscall.ECONNREFUSED)`.
3. Update the "new flow from" log line in `udpReadLoop` to read "new internal flow attempt from".
4. Update the "flow … expired" log line in `udpFlowSweeper` to read "internal flow attempt … expired".
5. Run `go build ./...` to verify compilation.
6. Commit and push.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/proxy/udp.go` | Modify | Add imports; unify retryable error check; update two log strings |

## Key Code Snippets

```go
isRetryable := (func() bool {
    if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
        return true
    }
    return errors.Is(readErr, syscall.ECONNREFUSED)
})()
if isRetryable {
    if attempt < udpFirstDatagramRetries {
        log.Printf("proxy: udp: upstream %s not ready, retrying (%d/%d)…", ...)
        continue
    }
    log.Printf("proxy: udp: upstream %s did not respond after %d attempts; continuing", ...)
    break
}
// Non-retryable read error (connection closed, etc.)
log.Printf("proxy: udp: upstream read error for %s: %v", ...)
break
```

## Risks & Open Questions

None — the change is confined to the retry error-classification logic and two log strings.
The existing retry count/interval constants are unchanged.

## Deviations from Plan

None.
