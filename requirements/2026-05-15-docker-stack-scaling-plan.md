# Docker Stack Service Scaling — Implementation Plan

**Requirement**: [2026-05-15-docker-stack-scaling.md](2026-05-15-docker-stack-scaling.md)
**Date**: 2026-05-15
**Status**: Draft

## Implementation Steps

1. Update requirement status to "In Progress" in both `_index.md` and the requirement file.
2. Add `DesiredReplicas int` field to `types.TargetInfo` — the single discriminator used throughout to identify swarm services vs plain containers.
3. Extend `config.ServiceEntry` with `Scale *int` field; propagate to `DesiredReplicas` in `entryToTargetInfo`; preserve `DesiredReplicas` in `Apply()` (add alongside the existing preserved fields).
4. Extend `docker.Manager` struct with a `swarmServices map[string]int` (serviceID → desiredReplicas); initialise in `NewManager`.
5. Add private helpers to `docker.Manager`: `registerSwarmService`, `unregisterSwarmService`, `isSwarmService`.
6. Add `docker.Manager.DiscoverServices(ctx, handler)` — lists swarm services labelled `lazy-tcp-proxy.enabled=true`, calls `serviceToTargetInfo`, joins overlay networks, calls `handler.RegisterTarget`.
7. Add `docker.Manager.serviceToTargetInfo(ctx, svc swarm.Service) (types.TargetInfo, error)` — parses all labels (same set as containers), reads VIP network IDs, sets `DesiredReplicas`, sets `Running` from `ServiceStatus.RunningTasks > 0`.
8. Modify `docker.Manager.EnsureRunning` to branch on `swarmServices`: if service, call `ensureServiceRunning` (uses `ServiceInspect` + `ServiceUpdate` to scale to N replicas); otherwise use existing container path.
9. Modify `docker.Manager.StopContainer` to branch on `swarmServices`: if service, call `stopService` (uses `ServiceUpdate` to scale to 0); otherwise use existing container path.
10. Modify `docker.Manager.GetUpstreamHost` to branch on `swarmServices`: if service, call `getServiceUpstreamHost` (iterates `service.Endpoint.VirtualIPs`, returns VIP IP for a joined overlay network); otherwise use existing container path.
11. Add `docker.Manager.WatchServiceEvents(ctx, handler)` — subscribes to `type=service` Docker events (`create`, `update`, `remove`), mirrors the structure of `WatchEvents` with exponential-backoff reconnect.
12. Add `docker.Manager.NotifyTargets(targets []types.TargetInfo)` — called after config overlay to ensure YAML-only service entries with `DesiredReplicas > 0` are registered in `swarmServices`.
13. Extend the `backendManager` interface in `main.go` with `DiscoverServices`, `WatchServiceEvents`, and `NotifyTargets`.
14. Update `discoverAndApply` in `main.go` to call `DiscoverServices` (non-fatal warning on error) and `NotifyTargets` (after `Apply`).
15. Start a `WatchServiceEvents` goroutine in `main()` alongside the existing `WatchEvents` goroutine.
16. Add no-op implementations of `DiscoverServices`, `WatchServiceEvents`, and `NotifyTargets` to the K8s backend so it satisfies the updated interface.
17. Run `go build` and `go vet` to verify no compilation errors.
18. Update requirement file status to "Completed" and plan file status to "Implemented".

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `requirements/2026-05-15-docker-stack-scaling.md` | Modify | Status: Planned → In Progress → Completed |
| `requirements/_index.md` | Modify | Status: Planned → In Progress → Completed |
| `lazy-tcp-proxy/internal/types/types.go` | Modify | Add `DesiredReplicas int` to `TargetInfo` |
| `lazy-tcp-proxy/internal/config/store.go` | Modify | Add `Scale *int` to `ServiceEntry`; propagate in `entryToTargetInfo`; preserve in `Apply()` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Swarm service discovery, scale-up/down, VIP lookup, service event watching, registry map |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Add no-op `DiscoverServices`, `WatchServiceEvents`, `NotifyTargets` |
| `lazy-tcp-proxy/main.go` | Modify | Extend `backendManager` interface; update `discoverAndApply`; start service-events goroutine |

## API Contracts

No new HTTP endpoints. The existing `/status` endpoint and dashboard automatically reflect swarm services because they read from `proxy.Snapshot()`, which iterates `targetState` entries that now include service-backed ports.

## Data Models

### `types.TargetInfo` (after change)
```go
type TargetInfo struct {
    ContainerID     string         // Docker container ID or swarm service ID
    ContainerName   string
    Ports           []PortMapping
    UDPPorts        []PortMapping
    NetworkIDs      []string       // overlay network IDs (for both containers and services)
    AllowList       []net.IPNet
    BlockList       []net.IPNet
    IdleTimeout     *time.Duration
    StartTimeout    *time.Duration
    Running         bool           // containers: State.Running; services: RunningTasks > 0
    WebhookURL      string
    Dependants      []string
    CronStart       string
    CronStop        string
    HTTPHealthCheck string
    HasHealthCheck  bool           // always false for swarm services
    DesiredReplicas int            // 0 = plain container; ≥ 1 = swarm service (scale-to value)
}
```

### `config.ServiceEntry` (after change)
```go
type ServiceEntry struct {
    Name             string   `yaml:"name"                         json:"name"`
    Ports            []string `yaml:"ports,omitempty"              json:"ports,omitempty"`
    UDPPorts         []string `yaml:"udp_ports,omitempty"          json:"udp_ports,omitempty"`
    AllowList        []string `yaml:"allow_list,omitempty"         json:"allow_list,omitempty"`
    BlockList        []string `yaml:"block_list,omitempty"         json:"block_list,omitempty"`
    IdleTimeoutSecs  *int     `yaml:"idle_timeout_secs,omitempty"  json:"idle_timeout_secs,omitempty"`
    StartTimeoutSecs *int     `yaml:"start_timeout_secs,omitempty" json:"start_timeout_secs,omitempty"`
    WebhookURL       string   `yaml:"webhook_url,omitempty"        json:"webhook_url,omitempty"`
    Dependants       []string `yaml:"dependants,omitempty"         json:"dependants,omitempty"`
    CronStart        string   `yaml:"cron_start,omitempty"         json:"cron_start,omitempty"`
    CronStop         string   `yaml:"cron_stop,omitempty"          json:"cron_stop,omitempty"`
    HTTPHealthCheck  string   `yaml:"http_healthcheck,omitempty"   json:"http_healthcheck,omitempty"`
    Scale            *int     `yaml:"scale,omitempty"              json:"scale,omitempty"`  // NEW
}
```

### `docker.Manager` (after change)
```go
type Manager struct {
    cli           *client.Client
    selfID        string
    mu            sync.Mutex
    joinedNets    map[string]string // networkID → name
    swarmServices map[string]int    // serviceID → desiredReplicas (NEW)
}
```

### `backendManager` interface (after change)
```go
type backendManager interface {
    Discover(ctx context.Context, handler types.TargetHandler) error
    DiscoverServices(ctx context.Context, handler types.TargetHandler) error  // NEW
    WatchEvents(ctx context.Context, handler types.TargetHandler)
    WatchServiceEvents(ctx context.Context, handler types.TargetHandler)      // NEW
    EnsureRunning(ctx context.Context, targetID string) error
    StopContainer(ctx context.Context, targetID, targetName string) error
    GetUpstreamHost(ctx context.Context, targetID, hint string) (string, error)
    WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error
    Shutdown(ctx context.Context)
    DefaultTargetID(name string) string
    NotifyTargets(targets []types.TargetInfo)  // NEW
}
```

## Key Code Snippets

### `serviceToTargetInfo` (Docker manager)

```go
func (m *Manager) serviceToTargetInfo(_ context.Context, svc swarm.Service) (types.TargetInfo, error) {
    labels := svc.Spec.Annotations.Labels

    scaleStr, hasScale := labels["lazy-tcp-proxy.scale"]
    if !hasScale || strings.TrimSpace(scaleStr) == "" {
        return types.TargetInfo{}, fmt.Errorf("missing label lazy-tcp-proxy.scale")
    }
    scale, err := strconv.Atoi(strings.TrimSpace(scaleStr))
    if err != nil || scale < 1 {
        return types.TargetInfo{}, fmt.Errorf("invalid lazy-tcp-proxy.scale %q: must be a positive integer ≥ 1", scaleStr)
    }

    portsStr, hasPorts := labels["lazy-tcp-proxy.ports"]
    udpPortsStr, hasUDPPorts := labels["lazy-tcp-proxy.udp-ports"]
    if !hasPorts && (!hasUDPPorts || udpPortsStr == "") {
        return types.TargetInfo{}, fmt.Errorf("missing label lazy-tcp-proxy.ports or lazy-tcp-proxy.udp-ports")
    }
    var ports []types.PortMapping
    if hasPorts {
        ports = types.ParsePortMappings("lazy-tcp-proxy.ports", portsStr)
        if len(ports) == 0 {
            return types.TargetInfo{}, fmt.Errorf("label lazy-tcp-proxy.ports contains no valid port mappings")
        }
    }
    var udpPorts []types.PortMapping
    if hasUDPPorts && udpPortsStr != "" {
        udpPorts = types.ParsePortMappings("lazy-tcp-proxy.udp-ports", udpPortsStr)
    }

    name := svc.Spec.Name

    // Extract network IDs from VirtualIPs (one VIP per attached overlay network)
    var networkIDs []string
    for _, vip := range svc.Endpoint.VirtualIPs {
        if vip.NetworkID != "" {
            networkIDs = append(networkIDs, vip.NetworkID)
        }
    }

    // ... (allow/block lists, timeouts, webhook, dependants, cron, http-healthcheck — same as containerToTargetInfo)

    running := svc.ServiceStatus != nil && svc.ServiceStatus.RunningTasks > 0

    return types.TargetInfo{
        ContainerID:     svc.ID,
        ContainerName:   name,
        Ports:           ports,
        UDPPorts:        udpPorts,
        NetworkIDs:      networkIDs,
        // ... other fields
        Running:         running,
        HasHealthCheck:  false,
        DesiredReplicas: scale,
    }, nil
}
```

### `EnsureRunning` branch (Docker manager)

```go
func (m *Manager) EnsureRunning(ctx context.Context, targetID string) error {
    if desiredReplicas, ok := m.isSwarmService(targetID); ok {
        return m.ensureServiceRunning(ctx, targetID, desiredReplicas)
    }
    // ... existing container logic unchanged
}

func (m *Manager) ensureServiceRunning(ctx context.Context, serviceID string, desiredReplicas int) error {
    result, err := m.cli.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{InsertDefaults: true})
    if err != nil {
        return fmt.Errorf("inspecting service: %w", err)
    }
    svc := result.Service
    if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil &&
        *svc.Spec.Mode.Replicated.Replicas > 0 {
        return nil // already scaled up
    }
    name := svc.Spec.Name
    log.Printf("docker: scaling up service \033[33m%s\033[0m to %d replicas", name, desiredReplicas)
    n := uint64(desiredReplicas)
    svc.Spec.Mode.Replicated = &swarm.ReplicatedService{Replicas: &n}
    if _, err := m.cli.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
        Version: svc.Meta.Version,
        Spec:    svc.Spec,
    }); err != nil {
        return fmt.Errorf("scaling up service: %w", err)
    }
    log.Printf("docker: service \033[33m%s\033[0m scaled to %d replicas", name, desiredReplicas)
    return nil
}
```

### `StopContainer` branch (Docker manager)

```go
func (m *Manager) StopContainer(ctx context.Context, targetID, targetName string) error {
    if _, ok := m.isSwarmService(targetID); ok {
        return m.stopService(ctx, targetID, targetName)
    }
    // ... existing container logic unchanged
}

func (m *Manager) stopService(ctx context.Context, serviceID, serviceName string) error {
    result, err := m.cli.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
    if err != nil {
        return fmt.Errorf("inspecting service: %w", err)
    }
    svc := result.Service
    if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil &&
        *svc.Spec.Mode.Replicated.Replicas == 0 {
        return nil // already scaled to zero
    }
    log.Printf("docker: scaling down service \033[33m%s\033[0m (idle timeout)", serviceName)
    n := uint64(0)
    svc.Spec.Mode.Replicated = &swarm.ReplicatedService{Replicas: &n}
    if _, err := m.cli.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
        Version: svc.Meta.Version,
        Spec:    svc.Spec,
    }); err != nil {
        return fmt.Errorf("scaling down service: %w", err)
    }
    log.Printf("docker: service \033[33m%s\033[0m scaled to zero", serviceName)
    return nil
}
```

### `GetUpstreamHost` branch (Docker manager)

```go
func (m *Manager) GetUpstreamHost(ctx context.Context, targetID, preferNetworkID string) (string, error) {
    if _, ok := m.isSwarmService(targetID); ok {
        return m.getServiceUpstreamHost(ctx, targetID, preferNetworkID)
    }
    // ... existing container logic unchanged
}

func (m *Manager) getServiceUpstreamHost(ctx context.Context, serviceID, preferNetworkID string) (string, error) {
    result, err := m.cli.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
    if err != nil {
        return "", fmt.Errorf("inspecting service: %w", err)
    }
    vips := result.Service.Endpoint.VirtualIPs

    // Prefer the hinted network
    if preferNetworkID != "" {
        for _, vip := range vips {
            if vip.NetworkID == preferNetworkID && vip.Addr.IsValid() {
                return vip.Addr.Addr().String(), nil
            }
        }
    }
    // Fallback: any VIP on a network the proxy has joined
    m.mu.Lock()
    joinedNets := m.joinedNets
    m.mu.Unlock()
    for _, vip := range vips {
        if _, joined := joinedNets[vip.NetworkID]; joined && vip.Addr.IsValid() {
            return vip.Addr.Addr().String(), nil
        }
    }
    // Last resort: any non-empty VIP
    for _, vip := range vips {
        if vip.Addr.IsValid() {
            return vip.Addr.Addr().String(), nil
        }
    }
    return "", fmt.Errorf("no VIP found for service %s", serviceID[:12])
}
```

### `WatchServiceEvents` (Docker manager)

The method mirrors `WatchEvents` but filters `type=service` and handles `create`, `update`, `remove` actions:

- **create**: inspect service, validate labels, register in `swarmServices` map, join networks, call `handler.RegisterTarget`
- **update**: if the service is in `swarmServices`, inspect to check `Spec.Mode.Replicated.Replicas`; if replicas == 0, call `handler.ContainerStopped`
- **remove**: if in `swarmServices`, call `unregisterSwarmService` then `handler.RemoveTarget`

Note: `ContainerStarted` is NOT called from `update` events because it would trigger spurious cascade starts. External scale-up is handled naturally: on the next connection, `EnsureRunning` sees replicas > 0 and returns nil, and the dial succeeds.

### Swarm mode detection in `DiscoverServices`

```go
infoResult, err := m.cli.Info(ctx, client.InfoOptions{})
if err != nil {
    return fmt.Errorf("getting docker info: %w", err)
}
if infoResult.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
    log.Printf("docker: swarm mode not active; skipping service discovery")
    return nil
}
```

### `discoverAndApply` (main.go, after change)

```go
func discoverAndApply(ctx context.Context, mgr backendManager, store *config.Store, srv *proxy.ProxyServer) error {
    collector := &config.TargetCollector{}
    if err := mgr.Discover(ctx, collector); err != nil {
        return fmt.Errorf("discover: %w", err)
    }
    if err := mgr.DiscoverServices(ctx, collector); err != nil {
        log.Printf("discover services warning: %v", err) // non-fatal
    }
    merged, errs := store.Apply(collector.Targets(), mgr.DefaultTargetID)
    for _, e := range errs {
        log.Printf("config apply warning: %v", e)
    }
    mgr.NotifyTargets(merged) // sync swarm registry for YAML-only service entries
    srv.Update(merged)
    return nil
}
```

### `WatchServiceEvents` goroutine in `main()`

```go
go func() {
    mgr.WatchServiceEvents(ctx, srv)
}()
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `ParseScaleLabel` — valid | `"2"` | `2, nil` |
| `ParseScaleLabel` — zero | `"0"` | `0, error` |
| `ParseScaleLabel` — negative | `"-1"` | `0, error` |
| `ParseScaleLabel` — non-integer | `"two"` | `0, error` |
| `serviceToTargetInfo` — missing scale | no `lazy-tcp-proxy.scale` label | error |
| `serviceToTargetInfo` — missing ports | no ports labels | error |
| `serviceToTargetInfo` — valid | scale=2, ports=8080:80 | `DesiredReplicas=2, Ports=[{8080,80}]` |
| `Apply` with `Scale` | YAML entry `scale: 3` | `DesiredReplicas=3` in merged target |
| `entryToTargetInfo` — `Scale: nil` | no scale field | `DesiredReplicas=0` |

## Risks & Open Questions

1. **Overlay network attachability**: Overlay networks must be created with `--attachable` for a non-swarm container to join them. If the proxy is itself a swarm service, this is not an issue. Document this constraint prominently in the README.
2. **ingress network**: Docker Swarm's built-in `ingress` network appears in `VirtualIPs` for services with published ports. The proxy should not try to join the ingress network (it's managed by swarm and not attachable). The existing `JoinNetworks` gracefully logs and skips networks it cannot join, so this is handled automatically.
3. **Service update race**: `ServiceUpdate` requires the current `Version` to avoid conflicting writes. If two agents update a service concurrently, one will get a version conflict error. The existing `singleflight` deduplication in the proxy server mitigates concurrent `EnsureRunning` calls for the same service.
4. **Global services**: Services with `mode: global` (not `replicated`) cannot be scaled by replica count. The proxy should log a warning and skip them if `Spec.Mode.Replicated == nil`.
5. **`DefaultTargetID` for services**: YAML-only service entries use `defaultID(name) = name` (the service name) as the `ContainerID`. Docker Swarm accepts service names in `ServiceInspect`, so `ensureServiceRunning("myservice", 2)` will work correctly.
