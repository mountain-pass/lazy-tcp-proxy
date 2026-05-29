# Dashboard Compose & Image Archive Indicators

**Date Added**: 2026-05-29
**Priority**: Low
**Status**: Planned

## Problem Statement

The status dashboard shows container lifecycle state (running/stopped/missing) but gives
no indication of whether a container has Compose re-provisioning configured (REQ-088).
Operators have no way to see at a glance which services will self-recover if removed.

## Functional Requirements

1. Two new icon indicators appear in the **target column**, immediately after the existing
   status emoji, for every row in the dashboard:
   - **♻️** — a compose file exists for this service (`<COMPOSE_DIR>/<name>.yml` or `.yaml`)
   - **📦** — a Docker image archive exists (`<COMPOSE_DIR>/<name>.tar.gz`)
2. Each icon is **greyed out** (low opacity) when the corresponding file does not exist,
   so the column stays visually consistent regardless of configuration.
3. All status icons carry `title` tooltip attributes describing their meaning:

   | Icon | `title` text |
   |------|-------------|
   | 🟢 | `Container running` |
   | 🟠 | `Container idle` |
   | 🔴 | `Container stopped` |
   | ⚠️ | `Container missing` |
   | ♻️ (active) | `Compose file found` |
   | ♻️ (greyed) | `No compose file` |
   | 📦 (active) | `Docker image tar found` |
   | 📦 (greyed) | `No docker image tar` |

4. The example layout for a service with both files present:
   ```
   🔴♻️📦 multilit-pdf-generator:3002
   ```

## User Experience Requirements

- Icons are always rendered in the same positions so the column does not shift between rows.
- Greyed icons use `opacity: 0.25` to remain visible but clearly inactive.
- Title tooltips appear on hover (standard HTML `title` attribute, no custom JS needed).

## Technical Requirements

- `TargetSnapshot` gains two new boolean fields: `has_compose_file` and `has_tar_gz`.
- `ProxyServer` gains a `SetComposeDir(dir string)` method. It checks file existence
  (`os.Stat`) when building snapshots — no Docker dependency required.
- `main.go` calls `srv.SetComposeDir(composeDir)` immediately after `mgr.SetComposeDir`.
- Only the docker build path is affected; the k8s binary will have `composeDir = ""`
  and both fields will always be `false`.
- The `TargetSnapshot` fields must be emitted even when `false` (omitempty NOT used) so
  the JavaScript can unconditionally read them.

## Acceptance Criteria

- [ ] Both new fields appear in the `/status` JSON response.
- [ ] `has_compose_file: true` when `<COMPOSE_DIR>/<name>.yml` or `.yaml` exists.
- [ ] `has_tar_gz: true` when `<COMPOSE_DIR>/<name>.tar.gz` exists.
- [ ] Dashboard renders ♻️ and 📦 in full opacity when the file exists, greyed otherwise.
- [ ] All four status emojis (🟢 🟠 🔴 ⚠️) have correct `title` attributes.
- [ ] Both new icons have correct `title` attributes in both states.
- [ ] Build and lint pass.

## Dependencies

- Depends on REQ-088 (Compose re-provisioning) for the `COMPOSE_DIR` convention.
