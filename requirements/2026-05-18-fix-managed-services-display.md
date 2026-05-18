# Fix: Config-Only Services Always Show as Stopped

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Completed

## Problem Statement

Services configured via `config.yml` (without the `lazy-tcp-proxy.enabled=true` Docker label) always
show as "stopped" in the `/status` web endpoint and dashboard, even when `docker ps` confirms the
container is running. There are two distinct causes:

1. **Wrong initial running state**: `config.Store.Apply()` creates a `TargetInfo` with `Running: false`
   (the zero value) for any YAML-only entry that has no matching discovered container. The actual
   container state is never fetched from Docker at startup.

2. **Runtime state never updated**: `docker.Manager.WatchEvents()` filters on the
   `lazy-tcp-proxy.enabled=true` label. Config-only containers don't carry this label, so their
   `start` and `die` events are dropped. The proxy's view of their running state never changes after
   initial discovery.

## Functional Requirements

1. At startup (after `discoverAndApply`), config-only targets must have their `Running` field
   populated from the actual Docker container state, not left as the default `false`.

2. At runtime, when a config-only container starts or stops externally, the proxy must update its
   internal running state accordingly (so the dashboard stays in sync).

## User Experience Requirements

- The `/status` endpoint and dashboard should accurately reflect whether a config-only managed
  container is running or stopped.
- No change to labels, config file format, or environment variables is required.

## Technical Requirements

- Add `InspectRunning(ctx context.Context, targetID string) (bool, error)` to the `backendManager`
  interface, implemented in `docker.Manager` (via `ContainerInspect`) and in `k8s.Backend`.
- In `discoverAndApply`, after `store.Apply()` returns the merged list, iterate over targets whose
  `Running` field was not set by the discovery phase (YAML-only entries — identified by them not
  being present in the collector's results before Apply was called). Call `InspectRunning` for each
  and update the `Running` field before `srv.Update(merged)`.
- Extend `WatchEvents` (or add a companion watcher) to also handle `start`/`die`/`destroy` events
  for config-only containers. The approach: after initial `discoverAndApply`, track the set of
  config-only container names and watch events for those containers in addition to labeled ones.
  When an event fires for a known config-only container, call `handler.ContainerStarted` /
  `handler.ContainerStopped` / `handler.RemoveTarget` as appropriate.
- The set of config-only names must be refreshed when `discoverAndApply` is re-run (e.g. on admin
  reload). The watcher must be restarted or updated accordingly.

## Acceptance Criteria

- [ ] A container defined only in `config.yml` (no `lazy-tcp-proxy.enabled` label), which is
      already running when the proxy starts, shows as running (idle or up) in `/status`.
- [ ] A container defined only in `config.yml` that is stopped when the proxy starts shows as
      stopped in `/status`.
- [ ] Starting a config-only container externally (e.g. `docker start selenium`) causes the
      dashboard to flip from stopped → idle within one poll cycle or event delivery.
- [ ] Stopping a config-only container externally causes the dashboard to flip from idle → stopped.
- [ ] Containers with the `lazy-tcp-proxy.enabled=true` label continue to behave exactly as before.
- [ ] All existing tests continue to pass.

## Dependencies

- REQ-065 (Dynamic Configuration File — the config overlay system being fixed)
- REQ-077 (Auto-Join Docker Networks for Static Config Targets — uses the same YAML-only detection logic)

## Implementation Notes

- The YAML-only targets can be identified in `discoverAndApply` by comparing the merged list against
  the set of names returned by `collector.Targets()` before `Apply` runs: any name in `merged` that
  is not in the collector's result set was added by the config overlay.
- For the Docker backend, `InspectRunning` is a thin wrapper around `ContainerInspect` returning
  `State.Running`.
- For the Kubernetes backend, `InspectRunning` can return `false, nil` as a safe stub initially
  (K8s running state is already populated via label discovery).
- The event watcher enhancement requires knowing the live set of config-only names. One approach:
  store this set in `main.go` after each `discoverAndApply` call, protected by a mutex, and pass a
  lookup function to `WatchEvents`.
