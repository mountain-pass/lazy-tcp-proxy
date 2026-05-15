# Docker Stack Service Scaling

**Date Added**: 2026-05-15
**Priority**: High
**Status**: In Progress

## Problem Statement

The lazy-tcp-proxy currently manages plain Docker containers — starting and stopping individual container instances. Docker Swarm stack services are a different abstraction: a service can run N replicas simultaneously, and scaling means adjusting that replica count rather than toggling a single container on/off.

Users who deploy services via `docker stack deploy` want the same lazy-scaling behaviour — hold services at 0 replicas when idle and scale them up to a configurable count (e.g. 2) when a connection arrives — without having to switch to Kubernetes or manage individual containers.

## Functional Requirements

1. The proxy MUST discover Docker Swarm services that carry the label `lazy-tcp-proxy.enabled=true` AND `lazy-tcp-proxy.scale=<N>` (N ≥ 1).
2. When a connection arrives for an idle service (0 replicas), the proxy MUST scale the service to the configured replica count using the Docker Swarm API (`ServiceUpdate`).
3. When all connections are closed and the idle timeout elapses, the proxy MUST scale the service back to 0 replicas.
4. The proxy MUST join the service's overlay networks (same mechanism as for containers) so it can reach the service VIP.
5. The upstream address for a swarm service MUST be the service's Virtual IP (VIP) on the shared overlay network, on the configured target port.
6. The proxy MUST watch Swarm service events (`type=service`) so it can react to externally-triggered scaling changes (e.g. a service scaled to 0 from outside the proxy).
7. Both plain Docker containers and Swarm services MUST be discoverable and managed simultaneously by the same running proxy instance (same binary and image).
8. The `scale` value MUST be configurable via:
   - The `lazy-tcp-proxy.scale` Docker label on the Swarm service definition.
   - The `scale` field in the YAML config file (`config.yaml`), following the existing dual-source overlay architecture.
9. All existing labels (`ports`, `udp-ports`, `allow-list`, `block-list`, `idle-timeout-secs`, `start-timeout-secs`, `webhook-url`, `dependants`, `cron-start`, `cron-stop`, `http-healthcheck`) MUST be supported on Swarm services with identical semantics.
10. If a swarm overlay network is not attachable, the proxy MUST log a clear warning rather than crashing.

## User Experience Requirements

- Docker Compose / stack file usage: add two labels to a service and a `scale` label with the desired replica count:
  ```yaml
  services:
    myapp:
      image: myapp:latest
      deploy:
        replicas: 0          # start at 0; proxy will scale up
        labels:
          lazy-tcp-proxy.enabled: "true"
          lazy-tcp-proxy.ports: "8080:80"
          lazy-tcp-proxy.scale: "2"
      networks:
        - mynetwork
  ```
- Log messages for scale-up and scale-down follow the same colour conventions as containers (`\033[33m` for service names).
- The `/status` HTTP endpoint and dashboard include swarm services alongside containers, showing replica count in place of the binary running/stopped state.

## Technical Requirements

- Uses Docker Swarm API only when swarm mode is active on the Docker daemon; gracefully skips service discovery when swarm is not enabled.
- Service discovery via `ServiceList` filtered by `lazy-tcp-proxy.enabled=true`.
- `lazy-tcp-proxy.scale` label value must be a positive integer (≥ 1). Invalid values are skipped with a warning log.
- Scale-up uses `ServiceUpdate` setting `Spec.Mode.Replicated.Replicas` to the configured value.
- Scale-down uses `ServiceUpdate` setting `Spec.Mode.Replicated.Replicas` to 0.
- Running-state detection: `ServiceInspect` (with `InsertDefaults: true`) checks `ServiceStatus.RunningTasks > 0`.
- Upstream host resolution: iterate `service.Endpoint.VirtualIPs`, find the entry whose `NetworkID` matches a network the proxy has joined, strip the CIDR mask, return the bare IP.
- Swarm overlay networks are joined using the existing `JoinNetworks()` mechanism.
- `TargetInfo` gains a new `DesiredReplicas int` field (0 = plain container, ≥ 1 = swarm service).
- `ServiceEntry` in the config YAML gains a `scale` integer field.
- Service events are watched alongside container events; the proxy subscribes to both `type=container` and `type=service` Docker event streams.

## Acceptance Criteria

- [ ] `docker stack deploy` with `lazy-tcp-proxy.enabled=true` and `lazy-tcp-proxy.scale=2` causes the service to be discovered at startup.
- [ ] An inbound TCP connection to the proxy while the service has 0 replicas triggers a scale-up to 2 replicas.
- [ ] After the idle timeout with no active connections, the service is scaled back to 0 replicas.
- [ ] An inbound UDP connection follows the same scale-up/scale-down lifecycle.
- [ ] The proxy correctly resolves the service VIP on the shared overlay network and forwards traffic to it.
- [ ] Scaling a service to 0 externally (outside the proxy) causes the proxy to record the service as stopped.
- [ ] A service scaled externally back to > 0 replicas causes the proxy to record it as running.
- [ ] Invalid `lazy-tcp-proxy.scale` values (e.g. `"0"`, `"foo"`, `"-1"`) are skipped with a clear warning; the service is not registered.
- [ ] Plain Docker containers continue to work correctly alongside swarm services.
- [ ] When swarm mode is not active, service discovery is silently skipped and container discovery proceeds normally.
- [ ] The `scale` field in `config.yaml` overrides the label value via the existing YAML overlay mechanism.
- [ ] The `/status` endpoint returns swarm services in its output.
- [ ] Log messages use the same colour conventions as existing container log messages.

## Dependencies

- Requires Docker daemon with Swarm mode enabled (for users who want swarm services; container mode remains independent).
- Requires swarm overlay networks to be created with `--attachable` if the proxy runs as a plain container (not as a swarm service itself). This constraint must be documented.
- Depends on the existing YAML config overlay architecture (REQ-065).
- Affects: `internal/types/types.go` (new field), `internal/docker/manager.go` (discovery + scale API), `internal/config/store.go` (new YAML field), `internal/proxy/server.go` (status output).

## Implementation Notes

- The `DesiredReplicas` field distinguishes swarm services from plain containers throughout the codebase: if `> 0`, use the Swarm API; if `== 0`, use the Container API.
- Swarm service IDs look identical to container IDs (64-char hex), so no prefix is needed for `ContainerID` — the manager uses `DesiredReplicas` to route API calls correctly.
- `WaitUntilHealthy` for swarm services polls `ServiceInspect` until `RunningTasks > 0`, reusing the same polling loop structure as Docker HEALTHCHECK polling.
- UDP traffic forwarding is unchanged; the VIP is reachable for UDP as well.
