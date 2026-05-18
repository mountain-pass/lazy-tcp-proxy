# Fix golangci-lint staticcheck QF1008 Violations in docker/manager.go

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Completed

## Problem Statement

The `Go CI` Lint job was failing with three `staticcheck QF1008` violations in
`internal/docker/manager.go`: redundant embedded-field selectors that can be
simplified by removing the intermediate embedded struct name.

## Functional Requirements

- `golangci-lint run` must pass with no issues.
- No change in runtime behaviour.

## Technical Requirements

- Line 530: `svc.Meta.Version` → `svc.Version`
- Line 556: `svc.Meta.Version` → `svc.Version`
- Line 663: `svc.Spec.Annotations.Labels` → `svc.Spec.Labels`

## Acceptance Criteria

- [x] `go vet ./...` exits 0
- [x] `go build ./...` exits 0
- [x] golangci-lint reports 0 issues

## Dependencies

None.

## Implementation Notes

`swarm.Service` embeds `Meta` directly, and `swarm.ServiceSpec` embeds
`Annotations` directly, so the intermediate field names are redundant.
