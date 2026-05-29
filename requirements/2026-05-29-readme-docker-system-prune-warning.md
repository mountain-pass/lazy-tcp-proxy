# README Warning: `docker system prune` Removes Stopped Containers

**Date Added**: 2026-05-29
**Priority**: Medium
**Status**: Completed

## Problem Statement

`docker system prune` silently removes all stopped containers by default. Because `lazy-tcp-proxy` routinely stops idle containers to save resources, any managed container that happens to be idle at prune time will be permanently deleted — with no warning to the user. Users unfamiliar with this behaviour may be confused when their services disappear.

Additionally, when a config-only container (registered via `config.yaml`) is destroyed, the proxy keeps the listener alive and marks the target as missing, showing ⚠️ on the status dashboard — but this distinction was not documented anywhere.

## Functional Requirements

- README.md must include an explicit warning that `docker system prune` (and `docker container prune`) will delete stopped containers, including those managed by the proxy.
- The warning must explain that config-only containers show ⚠️ on the status dashboard when missing and recover automatically when recreated.
- The `/status` JSON documentation must include the `container_missing` field introduced alongside the ⚠️ status icon.

## User Experience Requirements

- The warning should be prominent and easy to find — placed in a dedicated **Caveats** section in `README.md`.
- Language should be clear and actionable, telling the user what not to do and what to do instead.

## Technical Requirements

- No code changes required; this is a documentation-only update.
- `container_missing` field (added in the same session) must be reflected in the `/status` JSON example.

## Acceptance Criteria

- [x] `README.md` contains a **Caveats** section with a warning about `docker system prune`
- [x] The warning explains that stopped containers are removed by `docker system prune`
- [x] The warning mentions the ⚠️ dashboard icon for missing config-only containers
- [x] The `/status` JSON example includes the `container_missing` field

## Dependencies

- Depends on the ⚠️ missing-container status icon (same session), which introduced the `container_missing` field in the `/status` JSON response.

## Implementation Notes

Design, Plan, and Build phases were combined in a single step because the change is a documentation-only addition with no ambiguity or technical risk. The Caveats section was inserted between the Q&A link and the Features list in `README.md`. The `/status` JSON example was updated inline with the existing Status Endpoint section.
