# Auto-Join Docker Networks for Static Config Targets

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Completed

## Problem Statement

When a target is defined in `config.yaml` rather than via Docker labels, lazy-tcp-proxy registers it as a proxy target but never joins the Docker network(s) that the target container is connected to. This means the proxy cannot reach the container over Docker networking.

Example: `selenium-chrome` is defined in `config.yaml` and is connected to `selenium-chromium_default`. The proxy registers the target (`proxy: registered target selenium-chrome, TCP 4444->4444`) but never joins `selenium-chromium_default`, so connections fail.

Root cause: `JoinNetworks()` is only called inside `Discover()`, which only processes containers with the `lazy-tcp-proxy.enabled=true` Docker label. `store.Apply()` produces YAML-only targets with empty `NetworkIDs`, and nothing subsequently joins their networks.

## Functional Requirements

1. After initial discovery and config overlay, the proxy must look up each YAML-only target container by name in Docker and join all of its networks.
2. A YAML-only target is one whose `NetworkIDs` is empty after `store.Apply()` — i.e. it was not found by Docker label scanning.
3. The lookup uses `ContainerInspect` by name and joins networks even if the container is stopped — Docker network membership is independent of running state, so the proxy will be on the right network when `EnsureRunning` starts the container.
4. The lookup must also happen on config reload (every call to `discoverAndApply()`).
5. If the container does not exist at all (never created), the join is skipped silently.
6. This behaviour is Docker-only; the Kubernetes backend must continue to work unchanged (no-op).

## User Experience Requirements

- Log output should follow existing patterns: `docker: joining network <name> (<id>)` (already emitted by `JoinNetworks`).
- No new user-facing configuration required.

## Technical Requirements

- Add `JoinNetworksForContainerNames(ctx context.Context, names []string)` to the `backendManager` interface in `main.go`.
- Docker manager (`internal/docker/manager.go`) implements this by:
  1. Calling `ContainerInspect` with the container name directly (Docker API accepts names as IDs).
  2. On 404 (container not found): log nothing, continue to next name.
  3. On any other error: log and continue.
  4. Extracting `NetworkIDs` from `NetworkSettings.Networks` (works for running and stopped containers).
  5. Calling the existing `JoinNetworks()` with those IDs.
- Kubernetes backend (`internal/k8s/backend.go`) implements this as a no-op.
- `discoverAndApply()` (main.go) calls `JoinNetworksForContainerNames()` after `store.Apply()`, passing the names of all merged targets whose `NetworkIDs` slice is empty.

## Acceptance Criteria

- [x] On startup, if `selenium-chrome` is in `config.yaml` and the container is running in `selenium-chromium_default`, the log shows `docker: joining network selenium-chromium_default (...)` and the proxy can reach the container.
- [x] On startup, if `selenium-chrome` is in `config.yaml` and the container is stopped, the log still shows `docker: joining network selenium-chromium_default (...)` and the proxy can reach the container once `EnsureRunning` starts it.
- [x] If the container does not exist at all, no error is logged (silent skip).
- [x] Containers discovered via Docker labels are unaffected (networks already joined in `Discover()`).
- [x] K8s mode is unaffected.
- [x] Config reload (admin API) re-triggers the join, picking up any new networks.

## Dependencies

- REQ-001 Core TCP Proxy for Docker Containers
- REQ-065 Dynamic Configuration File (YAML Override Store)

## Implementation Notes

`ContainerInspect` is called with the container name directly — Docker resolves names to containers, avoiding the substring-match pitfall of the `ContainerList` name filter.
