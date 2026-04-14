# Add `name:` Field to All Recipe Compose Files

**Date Added**: 2026-04-11
**Priority**: High
**Status**: Completed

## Problem Statement

All recipe compose files in `recipes/` lack a top-level `name:` field. When multiple
recipe files are run from the same directory, Docker Compose assigns them the same
project name (the directory name), causing them to share a default network
(`<dirname>_default`). Running `docker compose down` on any one recipe then deletes that
shared network, leaving containers from other recipes unable to restart.

This is the root cause of the pihole "network not found" error described in REQ-056.

## Functional Requirements

1. Every file in `recipes/` MUST have a top-level `name:` field.
2. The name MUST match the primary service name in the file (derived from the filename
   slug between `docker-compose.` and the port suffix).

## Acceptance Criteria

- [ ] All 27 recipe files contain a `name:` field as the first line.
- [ ] Each name matches the service slug from the filename.

## Dependencies

- REQ-058 — same root cause; this is the user-facing fix, REQ-058 is the proxy-side
  detection and warning.
- Renumbered from REQ-057 → REQ-059 (REQ-057 was claimed by main before merge).
