# Make ports/udp-ports Labels Optional (Cascade-Only Registration)

**Date Added**: 2026-04-16
**Priority**: Medium
**Status**: In Progress

## Problem Statement

REQ-045 implemented dependency cascade, but FR5 required every dependant to be
fully registered (i.e. carry `lazy-tcp-proxy.enabled=true` **and** at least one
port label). In practice, pure backend containers (e.g. `ollama`) need no
proxied port of their own — they only need to participate in start/stop
cascade. The runtime message:

```
proxy: cascade stop: open-webui → "ollama" not registered, skipping
```

…occurs because `ollama` lacks the port labels and so is never added to the
proxy's registry.

The fix: keep the requirement for `lazy-tcp-proxy.enabled=true` on any
container that should participate in cascade, but make `lazy-tcp-proxy.ports`
and `lazy-tcp-proxy.udp-ports` **optional**. A container with only the
`enabled` label is registered with no port listeners and participates solely in
cascade start/stop.

## Functional Requirements

1. **`lazy-tcp-proxy.ports` and `lazy-tcp-proxy.udp-ports` are optional** — A
   container with only `lazy-tcp-proxy.enabled=true` is registered successfully.
   No TCP or UDP listeners are opened for it.

2. **Cascade-only containers participate fully** — A port-less registered
   container can be started and stopped via cascade exactly like a port-bearing
   container.

3. **Running state is tracked for port-less containers** — `cascadeStop` must
   not attempt to stop a port-less container that is already stopped
   (silent no-op), just as it does for port-bearing containers.

4. **Works for both Docker and Kubernetes** — The port-optional rule applies to
   both `internal/docker/manager.go` and `internal/k8s/backend.go`.

5. **No change to cascade logic for port-bearing containers** — Existing
   behaviour for containers with ports is unaffected.

## User Experience Requirements

Users add only `lazy-tcp-proxy.enabled=true` to a backend container, with no
port labels required:

```yaml
services:
  ollama:
    image: ollama/ollama
    labels:
      - "lazy-tcp-proxy.enabled=true"
      # no ports label needed

  open-webui:
    labels:
      - "lazy-tcp-proxy.enabled=true"
      - "lazy-tcp-proxy.ports=9002:8080"
      - "lazy-tcp-proxy.dependants=ollama"
```

Log output at startup:

```
docker: init: found containers: ollama, open-webui
proxy: registered target ollama (cascade-only, no ports)
proxy: registered target open-webui, TCP 9002->8080
```

Log output on cascade stop:

```
proxy: cascade stop: open-webui → ollama
docker: stopping container ollama (idle timeout)
```

## Technical Requirements

- **`internal/docker/manager.go` — `containerToTargetInfo`**: Remove the error
  when neither `lazy-tcp-proxy.ports` nor `lazy-tcp-proxy.udp-ports` is present.
  Return a valid `TargetInfo` with empty `Ports`/`UDPPorts` slices.

- **`internal/docker/manager.go` — `WatchEvents`**: Remove the early `continue`
  that skips containers missing port labels after the `enabled=true` check.
  A container with only `lazy-tcp-proxy.enabled=true` should be registered.

- **`internal/k8s/backend.go` — `deploymentToTargetInfo`**: Same change —
  remove the error when both port annotations are absent.

- **`internal/proxy/server.go` — `dependantState` / `dependantStates`**: Add a
  small struct and a `map[string]*dependantState` (keyed by container ID) to
  `ProxyServer` to track running state for port-less containers. Port-bearing
  containers continue to track state in `targetState`/`udpListenerState`.

- **`internal/proxy/server.go` — `RegisterTarget`**: When
  `len(info.Ports) == 0 && len(info.UDPPorts) == 0`, populate `dependantStates`
  and `nameToID` only — do not attempt to open listeners.

- **`internal/proxy/server.go` — `ContainerStopped`**: Also set
  `dependantStates[containerID].running = false` if present.

- **`internal/proxy/server.go` — `ContainerStarted`**: Also set
  `dependantStates[containerID].running = true` if present.

- **`internal/proxy/server.go` — `cascadeStop`**: After checking `s.targets`
  and `s.udpTargets` for running state, also check `dependantStates`; update it
  to `running=false` after a successful stop.

- **`internal/proxy/server.go` — `cascadeStart`**: After a successful
  `EnsureRunning`, update `dependantStates[depID].running = true` if present.

- **`internal/proxy/server.go` — `RemoveTarget`**: Also delete from
  `dependantStates` if present.

## Acceptance Criteria

- [ ] A container with only `lazy-tcp-proxy.enabled=true` (no port labels) is
      registered at startup with no listeners opened, visible in logs.
- [ ] Cascade stop reaches a port-less registered container — no "not
      registered, skipping" log message.
- [ ] Cascade start reaches a port-less registered container.
- [ ] If a port-less dependant is already stopped, cascade stop is a silent
      no-op (no `StopContainer` call).
- [ ] A `die` Docker event for a port-less container updates its tracked
      running state.
- [ ] Containers with ports continue to work exactly as before (no regression).
- [ ] The k8s backend also accepts Deployments with no port annotations.
- [ ] Existing unit tests continue to pass.

## Dependencies

- REQ-045 (Dependency Cascade) — relaxes FR5 of that requirement.
- `internal/docker/manager.go`
- `internal/k8s/backend.go`
- `internal/proxy/server.go`

## Implementation Notes

- The `WatchEvents` "die" path already calls `ContainerStopped` for every
  dying container (no label check), so state updates happen automatically
  when a port-less container stops externally.
- `dependantStates` is only populated for port-less containers; port-bearing
  containers continue to track state through their existing `targetState` /
  `udpListenerState` structs.
- Cascade remains non-recursive (no transitive chains).
