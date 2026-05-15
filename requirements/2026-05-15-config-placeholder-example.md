# Config Placeholder — Commented Example

**Date Added**: 2026-05-15
**Priority**: Low
**Status**: Completed

## Problem Statement

When the config file does not exist at startup, the proxy creates an empty
`services: []` placeholder. This gives no guidance on what fields are available
or how to format entries.

## Functional Requirements

The auto-created placeholder should contain a commented-out example of every
available service option, so users can uncomment and edit what they need without
consulting external documentation.

## Acceptance Criteria

- [ ] When `CONFIG_PATH` does not exist, the created file contains a commented-out
      example entry covering all fields (`name`, `ports`, `udp_ports`, `allow_list`,
      `block_list`, `idle_timeout_secs`, `start_timeout_secs`, `webhook_url`,
      `dependants`, `cron_start`, `cron_stop`, `http_healthcheck`).
- [ ] The file is valid YAML (comments are ignored by the parser).
- [ ] `README_CONFIG.md` documents the placeholder content.

## Dependencies

Depends on: REQ-065 (Dynamic Configuration File)

## Implementation Notes

Phase gates skipped at user's explicit request ("please go ahead"). Change is a
one-line string update in `Store.Load()` (`internal/config/store.go`).
