# Ensure RECIPES_DIR and COMPOSE_DIR Exist on Startup

**Date Added**: 2026-06-04
**Priority**: Low
**Status**: Planned

## Problem Statement

If the directories referenced by `RECIPES_DIR` and `COMPOSE_DIR` do not exist when the proxy starts, features that depend on them (Portainer App Templates, Compose re-provisioning) fail silently or produce confusing errors. Creating the directories automatically on startup avoids manual setup steps and makes the proxy self-initialising.

## Functional Requirements

1. During startup, after resolving `RECIPES_DIR` and `COMPOSE_DIR`, the proxy creates each directory if it does not already exist (equivalent to `mkdir -p`).
2. If directory creation fails, the proxy logs a warning but continues — the directories are optional features.

## Acceptance Criteria

- [ ] If `RECIPES_DIR` does not exist on startup, it is created automatically.
- [ ] If `COMPOSE_DIR` does not exist on startup, it is created automatically.
- [ ] If both directories already exist, startup behaviour is unchanged.
- [ ] A failure to create either directory produces a log warning (not a fatal error).

## Dependencies

- REQ-088 (Compose Re-provision) — uses `COMPOSE_DIR`
- REQ-097 (Portainer App Templates) — uses `RECIPES_DIR`

## Implementation Notes

- Use `os.MkdirAll(path, 0o755)` which is a no-op when the directory exists.
- Creation should happen in `main()` immediately after the resolve calls, before any feature that reads from the directories.
