# Fix Go CI Build (Unkeyed PortMapping Struct Literals)

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Completed

## Problem Statement

The `Go CI` GitHub Actions workflow was failing at the `go vet` step because
`server_test.go` used unkeyed struct literals for `types.PortMapping` (e.g.
`{9000, 80}`). `go vet` flags these as a `composites` violation because adding
a new field to the struct would silently reorder the values.

## Functional Requirements

- `go vet ./...` must pass with no errors.
- All existing tests must continue to pass.

## Technical Requirements

- Replace the six unkeyed `PortMapping` composite literals in
  `internal/proxy/server_test.go` with keyed form:
  `{ListenPort: 9000, TargetPort: 80}`.

## Acceptance Criteria

- [x] `go vet ./...` exits 0
- [x] `go test -count=1 ./...` exits 0

## Dependencies

None.

## Implementation Notes

Six occurrences across three test functions (`TestTargetInfoEqual_TLSDiffers`,
`TestTargetInfoEqual_APIKeyDiffers`, `TestTargetInfoEqual_HTTPSAndAPIKeySame`)
were updated. No logic was changed.
