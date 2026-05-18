# Fix: Config-Only Container Disappears After docker compose up

**Date Added**: 2026-05-18
**Priority**: High
**Status**: In Progress

## Problem Statement

When `docker compose up` is run for a container managed via `config.yaml` only (no
`lazy-tcp-proxy.enabled` label), the container disappears from the proxy's web dashboard and
never reappears. Connections to its proxy ports fail until the proxy is restarted.

`docker compose up` recreates containers by:
1. Stopping the running container → `die` event
2. Removing the container → `destroy` event
3. Creating a new container → `create` event
4. Starting the new container → `start` event

In the current code (post REQ-079), `WatchEvents` handles `destroy` for config-only containers
by calling `handler.RemoveTarget(registeredID)`. This tears down all proxy listeners and
unregisters the target. When `start` then fires, `handler.ContainerStarted(registeredID)` is
called — but there is no longer a registered target to update, so the call is a silent no-op.
The container never reappears.

A secondary issue: after recreation, `docker compose up` creates a **new Docker network** for
the container. The proxy is not a member of this network, so even if the target were still
registered, connections would fail because the proxy cannot reach the new container IP.

## Functional Requirements

1. When a config-only container is destroyed and recreated (e.g. via `docker compose up`), it
   must reappear in the proxy dashboard automatically after the `start` event, without any
   proxy restart or admin reload.

2. After recreation, the proxy must be able to reach the new container (correct network
   membership).

## User Experience Requirements

- `docker compose up` on a config-only container should be transparent to users connecting
  through the proxy: a brief gap during recreation is acceptable, but the proxy must recover
  automatically.
- No change to `config.yaml` format, labels, or environment variables.

## Technical Requirements

- In `WatchEvents`, the `destroy` handler for config-only containers must call
  `handler.ContainerStopped(registeredID)` instead of `handler.RemoveTarget(registeredID)`.
  Config-only targets are defined in `config.yaml`, not by Docker labels, so destroying the
  container does not remove the config entry. The proxy should keep its listeners and port
  registrations, marking the target as stopped until it restarts.
- In `WatchEvents`, the `start` handler for config-only containers must inspect the newly
  started container, join any new networks, and then call `handler.ContainerStarted(registeredID)`
  to mark the target as running.
- The existing `RemoveTarget` path for config-only containers remains reachable only via
  `discoverAndApply` → `srv.Update()` (when the entry is removed from `config.yaml`).

## Acceptance Criteria

- [ ] Running `docker compose up` on a config-only container causes it to disappear briefly
      then reappear in the `/status` dashboard without proxy restart.
- [ ] After `docker compose up`, connections through the proxy to the config-only container
      succeed (proxy has joined the new network).
- [ ] Labeled containers (`lazy-tcp-proxy.enabled=true`) are unaffected.
- [ ] A config-only container that is genuinely removed (via `docker rm`) and NOT recreated
      is eventually cleaned up on the next `discoverAndApply` / admin reload.
- [ ] All existing tests continue to pass.

## Dependencies

- REQ-079 (Fix: Config-Only Services Always Show as Stopped — introduced `getConfigOnlyID` and
  the `WatchEvents` config-only routing that this fix extends)

## Implementation Notes

- Only two lines change in `WatchEvents`:
  1. `destroy` case: replace `handler.RemoveTarget(targetID)` with
     `handler.ContainerStopped(targetID)` when `rid != ""`.
  2. `start` case: before calling `handler.ContainerStarted(rid)`, call
     `m.cli.ContainerInspect` on `msg.Actor.ID`, extract network IDs, and call
     `m.JoinNetworks(ctx, networkIDs)`.
- No interface changes required.
