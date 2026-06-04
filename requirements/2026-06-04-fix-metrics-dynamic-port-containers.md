# Fix: /metrics Endpoint Missing Containers with Only Dynamic Port Mappings

**Date Added**: 2026-06-04
**Priority**: High
**Status**: In Progress

## Problem Statement

Containers that are registered exclusively via `lazy-tcp-proxy.traefik-hosts` or `lazy-tcp-proxy.traefik-tcp-hosts` labels (with no explicit `lazy-tcp-proxy.ports` or `lazy-tcp-proxy.udp-ports` labels) do not appear in the `/metrics` endpoint output.

The root cause is that dynamic port allocation (REQ-101) assigns listen ports and stores them in `TargetInfo.TraefikHosts` / `TargetInfo.TraefikTCPHosts`, but never adds corresponding entries to `TargetInfo.Ports`. Since `ProxyServer.RegisterTarget` creates TCP listeners only from `info.Ports`, these containers never enter `s.targets`, and therefore never appear in `ProxyServer.Snapshot()` — which powers `/metrics`.

## Functional Requirements

1. Containers registered exclusively via `traefik-hosts` or `traefik-tcp-hosts` labels must appear in the `/metrics` endpoint.
2. Each dynamically allocated listen port must be reflected as a `PortMapping` in `TargetInfo.Ports`, so the proxy server creates the correct TCP listener.
3. Both `traefik_hosts` (HTTP routes) and `traefik_tcp_hosts` (TCP SNI routes) use TCP listeners, so both contribute entries to `TargetInfo.Ports`.
4. Existing explicit `ports`/`udp-ports` behaviour must remain unchanged.
5. No duplicate port mappings: if the same listen port is already present in `Ports` (from an explicit label), it must not be added again.

## User Experience Requirements

- After the fix, the `/metrics` endpoint shows all managed containers, including those with only dynamically assigned ports.
- The `listen_port` and `target_port` fields in the metrics output correctly reflect the allocated listen port and the container target port.

## Technical Requirements

- Add a new method `AllocatePortMappings(specs []TraefikHostSpec) ([]string, []PortMapping)` to `PortAllocator` in `internal/types/portalloc.go`. It returns both the `domain:listen_port` strings (existing behaviour) and the corresponding `PortMapping` slice.
- In all three allocation call sites — `internal/docker/manager.go`, `internal/k8s/backend.go`, and `internal/config/store.go` — replace calls to `AllocateForHosts` with `AllocatePortMappings` and append the returned `PortMapping` slice to `ports`.
- Existing `AllocateForHosts` method may be kept (it delegates to the new method) or removed if no external callers remain.

## Acceptance Criteria

- [ ] A container with only `lazy-tcp-proxy.traefik-hosts=app.example.com:8080` appears in `/metrics` with the correct dynamically assigned `listen_port` and `target_port: 8080`.
- [ ] A container with only `lazy-tcp-proxy.traefik-tcp-hosts=db.example.com:5432` appears in `/metrics` with the correct dynamically assigned `listen_port` and `target_port: 5432`.
- [ ] A container with both explicit `ports` and `traefik-hosts` labels appears once per port (no duplicate mappings).
- [ ] All existing tests pass; new unit tests cover `AllocatePortMappings`.

## Dependencies

- REQ-101: Dynamic Port Allocation for `traefik_hosts` / `traefik_tcp_hosts` (the feature this fixes)
- REQ-095: Rename /status to /metrics and Add Memory Fields (the endpoint affected)

## Implementation Notes

- `AllocateForHosts` currently skips specs with empty `Domain`; `AllocatePortMappings` must do the same to keep output lengths consistent.
- Both traefik HTTP hosts and traefik TCP SNI hosts map to TCP listeners — only `TargetInfo.Ports` needs updating, not `TargetInfo.UDPPorts`.
