# Dynamic Port Allocation for traefik_hosts / traefik_tcp_hosts

**Date Added**: 2026-06-04
**Priority**: High
**Status**: Completed

## Problem Statement

When using Portainer app templates, the `PROXY_PORT` variable is parameterised per-service. Users can accidentally specify the same value for two different services, causing a silent port conflict that is only logged — the second service simply fails to register with no visible feedback to the user.

The root cause is that `traefik_hosts` and `traefik_tcp_hosts` currently require the user to specify the `listen_port` explicitly (e.g. `s3.yourdomain.com:3000`). This means the port must be coordinated manually across services, which is error-prone.

## Functional Requirements

1. The `traefik_hosts` and `traefik_tcp_hosts` label values change their format: instead of `domain:listen_port`, the port now represents the **target port on the container** (e.g. `s3.yourdomain.com:9000`).

2. lazy-tcp-proxy maintains an internal index of auto-assigned listen ports, starting at `LISTEN_START_PORT` (default `8000`, configurable via environment variable).

3. On registration, for each unique hostname in `traefik_hosts` / `traefik_tcp_hosts`, lazy-tcp-proxy dynamically assigns the next available listen port from the pool, incrementing from the last assigned port. Assignment order is determined by sorting container names alphabetically, ensuring determinism across restarts.

4. Ports already claimed by explicit `lazy-tcp-proxy.ports` or `lazy-tcp-proxy.udp-ports` labels are skipped when allocating dynamic ports.

5. Explicit `lazy-tcp-proxy.ports` / `lazy-tcp-proxy.udp-ports` labels continue to work exactly as before. Both mechanisms can coexist on the same container.

6. The validation rule is relaxed: a container is accepted if it has **at least one** of `ports`, `udp-ports`, `traefik_hosts`, or `traefik_tcp_hosts` present and valid.

7. The assigned listen port is resolved **before** the `TargetInfo` is registered with the proxy server, so `TraefikHosts` / `TraefikTCPHosts` in `TargetInfo` continue to carry `domain:listen_port` pairs (format unchanged internally; only the label input format changes).

## User Experience Requirements

- Users specify only the container target port in the label, e.g.:
  ```
  lazy-tcp-proxy.traefik_hosts=s3.yourdomain.com:9000
  ```
  No `PROXY_PORT` variable is needed; lazy-tcp-proxy assigns the listen port automatically.

- Users who need a fixed listen port continue to use `lazy-tcp-proxy.ports=8500:9000` as before.

- Both labels may be used together on the same container without conflict.

## Technical Requirements

- New environment variable: `LISTEN_START_PORT` (default `8000`). Parsed once at startup.
- Port allocator lives in `internal/config/store.go` (or a new `internal/portalloc` package) as a shared, mutex-protected counter.
- Allocation is performed during `inspectContainer` / `inspectService` / swarm event handling, after label parsing and before `RegisterTarget` is called.
- Allocated port is skipped if already in the explicit-ports set for any registered or in-process container.
- `ParseTraefikHosts` changes: the port suffix now validates that it is a valid integer (target port) but makes no claim about listen ports. The function name and signature stay the same; callers are responsible for converting to `domain:listen_port` pairs by injecting the allocated port.

## Acceptance Criteria

- [ ] `traefik_hosts=s3.yourdomain.com:9000` results in a listener on the auto-assigned port (e.g. `8000`) and a Traefik HTTP route for `s3.yourdomain.com` pointing at `proxy:8000`.
- [ ] `traefik_tcp_hosts=mongo.example.com:27017` results in a listener on the next auto-assigned port and a Traefik TCP SNI route.
- [ ] Two containers with `traefik_hosts` targeting different hostnames receive distinct listen ports.
- [ ] A container with both `ports=8500:9000` and `traefik_hosts=s3.yourdomain.com:9000` gets listen port `8500` from the explicit mapping; the allocator skips `8500`.
- [ ] A container with only `traefik_hosts` (no `ports`) is accepted by validation.
- [ ] A container with none of `ports`, `udp-ports`, `traefik_hosts`, `traefik_tcp_hosts` is rejected with a clear error.
- [ ] Port assignment order is deterministic: the same set of containers always receives the same ports after a restart.
- [ ] `LISTEN_START_PORT` env var changes the base port.
- [ ] Existing explicit-ports behaviour is unaffected.
- [ ] All existing tests pass; new unit tests cover the allocator and updated validation.

## Dependencies

- REQ-082: Traefik TCP SNI Routing (traefik_tcp_hosts label — introduces the label this changes)
- REQ-075: Traefik Entrypoint and CertResolver Configuration
- REQ-097: Portainer App Templates (motivating use case)

## Implementation Notes

- Assignment order: collect all containers/services being registered, sort by `ContainerName` ascending, then allocate ports in that order. For event-driven (single-container) registrations, each new hostname simply gets the next port after all currently claimed ports.
- The allocator must hold a global (process-level) set of claimed listen ports: union of explicit ports from all registered targets plus all dynamically assigned ports.
- On `RemoveTarget`, dynamically assigned ports are **not** freed — this avoids reassignment churn and keeps port-to-hostname mapping stable while Traefik refreshes.
