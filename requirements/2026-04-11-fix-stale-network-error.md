# Fix: Stale Docker Network Error on Container Start

**Date Added**: 2026-04-11
**Priority**: High
**Status**: Completed

## Problem Statement

When `docker compose down` is run on the proxy's compose stack, Docker Compose deletes
any networks it created — including the default project network (e.g. `myproject_default`).
If a managed container (e.g. pihole) was on that same default network (because both
compose files were run from the same directory without an explicit `name:` or `networks:`
block), the managed container now has a stale reference to a deleted network.

The next time a connection arrives and the proxy calls `docker start <container>`, Docker
returns:

```
Error response from daemon: failed to set up container networking: network <SHA> not found
```

The proxy propagates this as `"starting container: <error>"` with no indication of the
root cause or how to fix it.

There are two failure modes to address:

1. **Proactive**: detect at startup/discovery that a managed container shares the proxy's
   compose default network, and warn before anything breaks.
2. **Reactive**: when `ContainerStart` fails with "network … not found", emit an
   actionable hint so the operator knows what went wrong and how to recover.

## Functional Requirements

1. **Proactive warning** — during container discovery (and when new containers are
   registered via events), if a managed container is already on the same network as the
   proxy AND that network is a Docker Compose default network (label
   `com.docker.compose.network=default`), log a warning.
2. **Reactive hint** — when `ContainerStart` fails with an error containing both
   `"network"` and `"not found"`, log an additional actionable hint message before
   returning the original error.
3. Neither change alters returned errors or existing control flow.

## User Experience Requirements

**Proactive warning** (logged at discovery time):

```
docker: WARNING: container "pihole" shares the proxy's default compose network "myproject_default".
  Running "docker compose down" on the proxy stack will delete this network and leave "pihole" unable to restart.
  Fix: add a top-level "name:" field to each of your compose files to give them unique project names.
```

**Reactive hint** (logged when ContainerStart fails):

```
docker: container "pihole" has a stale network reference; recreate the container to fix this (docker rm pihole && docker compose up -d)
```

Note: the reactive hint must not assume compose file names — the `docker compose up -d`
command is given without a `-f` flag as a generic starting point.

## Technical Requirements

- **Proactive warning**: add a helper `warnSharedDefaultNetworks(ctx, info)` called from
  `Discover()` and from the `create`/`start` branch of `WatchEvents()`, both in
  `lazy-tcp-proxy/internal/docker/manager.go`.
  - For each network ID in `info.NetworkIDs`, inspect the network.
  - If the network label `com.docker.compose.network` equals `"default"` AND the proxy
    container (`m.selfID`) is already a member → log the warning.
  - Skip silently if `m.selfID` is empty (proxy not running inside Docker).
- **Reactive hint**: in `EnsureRunning()`, after `ContainerStart` returns an error,
  check `strings.Contains(err.Error(), "network") && strings.Contains(err.Error(), "not found")`;
  if true, log the hint before returning.
- All changes confined to `lazy-tcp-proxy/internal/docker/manager.go`.
- No new imports or dependencies required (`strings` is already imported).

## Acceptance Criteria

- [ ] At startup, if a managed container shares the proxy's compose default network,
      a warning is logged containing "shares", the network name, and "name:".
- [ ] The warning is also logged when a container is dynamically registered via events.
- [ ] No warning is logged when the managed container is on a different network from
      the proxy, or on a non-default compose network.
- [ ] When `ContainerStart` returns an error containing "network" and "not found",
      a hint log line is emitted mentioning "stale network reference" and "recreate".
- [ ] When `ContainerStart` fails for any other reason, no hint is logged.
- [ ] When `ContainerStart` succeeds, behaviour is unchanged.
- [ ] `go test ./...` continues to pass.

## Dependencies

- REQ-001 (Core TCP Proxy for Docker Containers) — UX improvements to discovery and
  container-start flow.

## Implementation Notes

- The `com.docker.compose.network=default` label is set by Docker Compose on
  auto-created project default networks. Explicitly defined named networks carry the
  actual network name instead, so the check is naturally scoped to the problematic case.
- Network membership is already inspected in `JoinNetworks()`; the new helper can reuse
  the same `NetworkInspect` call pattern.
- The `strings` package is already imported in `manager.go`.
