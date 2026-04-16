# Auto-Register Unlabeled Dependant Containers

**Date Added**: 2026-04-16
**Priority**: Medium
**Status**: Planned

## Problem Statement

REQ-045 implemented dependency cascade: when a managed container stops, all
containers listed in its `lazy-tcp-proxy.dependants` label are also stopped
(and vice versa on start). However, FR5 of REQ-045 restricted cascade to
containers that are themselves registered with the proxy (i.e. they also carry
`lazy-tcp-proxy.enabled=true`).

In practice many dependants are pure backend services (e.g. `ollama`, a
database, a worker) that need no proxy port of their own. Requiring users to
add `lazy-tcp-proxy.enabled=true` (and dummy port labels) to those containers
just to participate in cascade is counter-intuitive and unnecessary. The result
is the observed runtime message:

```
proxy: cascade stop: open-webui → "ollama" not registered, skipping
```

## Functional Requirements

1. **Auto-registration of referenced dependants** — During the Docker manager's
   `Discover` phase, after all labeled containers are registered, the proxy must
   inspect every unique container name listed in any `lazy-tcp-proxy.dependants`
   label that is not already registered and register it automatically with an
   empty port list (no TCP/UDP listeners created).

2. **No ports required for dependant-only containers** — A container registered
   solely because it is referenced as a dependant does not need any
   `lazy-tcp-proxy.*` labels. The proxy discovers it purely from another
   container's `dependants` label.

3. **Cascade start/stop works for port-less containers** — `cascadeStop` and
   `cascadeStart` in `ProxyServer` must handle dependants that have no managed
   listeners. Running-state for these containers is tracked separately so that
   an already-stopped dependant is not stopped again (silent no-op).

4. **External stop events update state** — When Docker fires a `die` event for
   a port-less dependant, `ContainerStopped` already routes it through the
   proxy (no label check on `die` events) and must update the dependant's
   tracked running state.

5. **Network join** — When a port-less dependant is auto-registered, the proxy
   joins its networks (same as for labeled containers), so that the proxy can
   reach it when cascade-starting.

6. **No changes to k8s backend** — This release targets Docker only; the
   Kubernetes backend already requires explicit labeling of all managed
   resources.

7. **Managed-only upstream unchanged** — Only the upstream (the labeled
   container with the `dependants` label) need be registered the traditional
   way. The dependant-only container requires no labels at all.

## User Experience Requirements

Before this change a user needed:

```yaml
# ollama — had to add labels just for cascade participation
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=11434:11434"   # dummy port

# open-webui
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9002:8080"
  - "lazy-tcp-proxy.dependants=ollama"
```

After this change:

```yaml
# ollama — no labels needed
# (no labels required)

# open-webui
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9002:8080"
  - "lazy-tcp-proxy.dependants=ollama"
```

Log output on startup:

```
docker: init: auto-registered dependant ollama (no proxy ports)
```

## Technical Requirements

- New method `containerToMinimalTargetInfoByName(ctx, name string)` in
  `internal/docker/manager.go` — inspects a container by name (no label
  requirements) and returns a `TargetInfo` with empty `Ports`/`UDPPorts`.
- `Discover` in `internal/docker/manager.go` — after the main label-filtered
  loop, collect all unregistered dependant names and call the new method for
  each, then call `handler.RegisterTarget(info)`.
- New struct `dependantState` and map `dependantStates map[string]*dependantState`
  in `internal/proxy/server.go` — tracks running state for port-less containers
  (those with no TCP or UDP port mappings). Keyed by container ID.
- `RegisterTarget` in `internal/proxy/server.go` — when `len(info.Ports) == 0`
  and `len(info.UDPPorts) == 0`, populate `dependantStates` instead of
  creating listeners. Still updates `nameToID`.
- `cascadeStop` in `internal/proxy/server.go` — when checking running state for
  a dependant, also consult `dependantStates`; update it to `running=false`
  after a successful stop.
- `cascadeStart` in `internal/proxy/server.go` — after a successful
  `EnsureRunning`, update `dependantStates` entry to `running=true` if present.
- `ContainerStopped` in `internal/proxy/server.go` — also set
  `dependantStates[containerID].running = false` if present.
- `RemoveTarget` in `internal/proxy/server.go` — also delete from
  `dependantStates` if present.

## Acceptance Criteria

- [ ] A container without `lazy-tcp-proxy.*` labels that is listed in another
      container's `lazy-tcp-proxy.dependants` is automatically registered at
      startup (visible in logs).
- [ ] Cascade stop reaches the unlabeled dependant — no "not registered,
      skipping" log message.
- [ ] Cascade start reaches the unlabeled dependant.
- [ ] If the dependant is already stopped when a cascade stop runs, no
      `StopContainer` call is made (silent no-op).
- [ ] A `die` Docker event for the unlabeled dependant updates its tracked
      running state so that a subsequent cascade stop is a no-op.
- [ ] Labeled containers with both `ports` and `dependants` labels continue
      to work exactly as before (no regression).
- [ ] Existing unit tests continue to pass.

## Dependencies

- REQ-045 (Dependency Cascade) — this extends FR5 of that requirement.
- `internal/docker/manager.go` — `Discover`, new helper method
- `internal/proxy/server.go` — `ProxyServer`, cascade logic, `dependantStates`

## Implementation Notes

- The `WatchEvents` "die" path already calls `ContainerStopped` for **every**
  dying container regardless of label, so state is updated automatically when
  `ollama` stops externally.
- The "start" event for unlabeled containers is currently skipped (label check
  present) — this is acceptable because cascade start always sets
  `running=true` explicitly after calling `EnsureRunning`.
- Cascade is still **not** recursive (no transitive chains).
- k8s backend not changed in this requirement.
