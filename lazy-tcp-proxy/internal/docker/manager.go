package docker

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
	cron "github.com/robfig/cron/v3"
)

// Manager wraps the Docker client with proxy-specific logic.
type Manager struct {
	cli           *client.Client
	selfID        string
	mu            sync.Mutex
	joinedNets    map[string]string // networkID → name
	swarmServices map[string]int    // serviceID → desiredReplicas
}

// NewManager creates a new Manager. The Docker socket path can be set via
// DOCKER_SOCK (e.g. /var/run/docker.sock). Falls back to DOCKER_HOST, then the
// default socket.
func NewManager() (*Manager, error) {
	opts := []client.Opt{client.FromEnv}
	if sock := os.Getenv("DOCKER_SOCK"); sock != "" {
		opts = append([]client.Opt{client.WithHost("unix://" + sock)}, opts...)
	}
	cli, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	m := &Manager{cli: cli, joinedNets: make(map[string]string), swarmServices: make(map[string]int)}
	m.selfID = m.SelfContainerID()
	if m.selfID != "" {
		log.Printf("docker: detected self container ID: %s", m.selfID)
	} else {
		log.Printf("docker: could not detect self container ID; network auto-join disabled")
	}
	return m, nil
}

// SelfContainerID reads the proxy's own container ID from /proc/self/cgroup,
// falling back to /etc/hostname, and returns "" if not determinable.
func (m *Manager) SelfContainerID() string {
	// Try /proc/self/cgroup first (docker sets long hex IDs here)
	f, err := os.Open("/proc/self/cgroup")
	if err == nil {
		defer f.Close() //nolint:errcheck
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Split(line, ":")
			if len(parts) < 3 {
				continue
			}
			cgroupPath := parts[2]
			base := filepath.Base(cgroupPath)
			// Docker container IDs are 64-char hex strings
			if len(base) == 64 {
				return base
			}
			// Also handle docker-<id>.scope format used by systemd cgroups v2
			if strings.HasPrefix(base, "docker-") && strings.HasSuffix(base, ".scope") {
				id := strings.TrimPrefix(base, "docker-")
				id = strings.TrimSuffix(id, ".scope")
				if len(id) == 64 {
					return id
				}
			}
		}
	}

	// Fallback: /etc/hostname often contains the short container ID
	data, err := os.ReadFile("/etc/hostname")
	if err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) == 12 || len(id) == 64 {
			return id
		}
	}

	return ""
}

// Discover lists all containers (running or stopped) that have the proxy label,
// joins their networks, and calls handler.RegisterTarget for each.
func (m *Manager) Discover(ctx context.Context, handler types.TargetHandler) error {
	f := make(client.Filters)
	f.Add("label", "lazy-tcp-proxy.enabled=true")

	containers, err := m.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	var foundNames []string
	var allNetworks []string
	for _, c := range containers.Items {
		info, err := m.containerToTargetInfo(ctx, c.ID)
		if err != nil {
			log.Printf("docker: discover: skipping container %s: %v", c.ID[:12], err)
			continue
		}

		m.warnSharedDefaultNetworks(ctx, info)

		joined, err := m.JoinNetworks(ctx, info.NetworkIDs)
		if err != nil {
			log.Printf("docker: discover: failed to join networks for \033[33m%s\033[0m: %v", info.ContainerName, err)
		}
		allNetworks = append(allNetworks, joined...)

		handler.RegisterTarget(info)
		foundNames = append(foundNames, info.ContainerName)
	}

	if len(foundNames) == 0 {
		log.Printf("docker: init: no proxy containers found")
	} else {
		log.Printf("docker: init: found containers: \033[33m%s\033[0m", strings.Join(foundNames, ", "))
	}
	if len(allNetworks) == 0 {
		log.Printf("docker: init: no networks joined")
	} else {
		log.Printf("docker: init: joined networks: \033[32m%s\033[0m", strings.Join(allNetworks, ", "))
	}

	return nil
}

// containerToTargetInfo inspects a container and builds a TargetInfo.
func (m *Manager) containerToTargetInfo(ctx context.Context, containerID string) (types.TargetInfo, error) {
	result, err := m.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return types.TargetInfo{}, fmt.Errorf("inspecting container: %w", err)
	}
	inspect := result.Container

	portsStr, hasPorts := inspect.Config.Labels["lazy-tcp-proxy.ports"]
	udpPortsStr, hasUDPPorts := inspect.Config.Labels["lazy-tcp-proxy.udp-ports"]
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

	name := strings.TrimPrefix(inspect.Name, "/")

	var networkIDs []string
	for _, ep := range inspect.NetworkSettings.Networks {
		if ep.NetworkID != "" {
			networkIDs = append(networkIDs, ep.NetworkID)
		}
	}

	var allowList, blockList []net.IPNet
	if v, ok := inspect.Config.Labels["lazy-tcp-proxy.allow-list"]; ok && v != "" {
		allowList = types.ParseIPList("lazy-tcp-proxy.allow-list", v)
	}
	if v, ok := inspect.Config.Labels["lazy-tcp-proxy.block-list"]; ok && v != "" {
		blockList = types.ParseIPList("lazy-tcp-proxy.block-list", v)
	}

	idleTimeout := types.ParseIdleTimeoutLabel(name, inspect.Config.Labels["lazy-tcp-proxy.idle-timeout-secs"])
	startTimeout := types.ParseStartTimeoutLabel(name, inspect.Config.Labels["lazy-tcp-proxy.start-timeout-secs"])

	var webhookURL string
	if v := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.webhook-url"]); v != "" {
		if _, err := url.ParseRequestURI(v); err != nil {
			log.Printf("docker: container %s: ignoring invalid webhook URL %q: %v", name, v, err)
		} else {
			webhookURL = v
		}
	}

	var dependants []string
	if v := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.dependants"]); v != "" {
		dependants = types.ParseDependants(v)
	}

	cronStart := parseCronLabel(name, "lazy-tcp-proxy.cron-start", inspect.Config.Labels["lazy-tcp-proxy.cron-start"])
	cronStop := parseCronLabel(name, "lazy-tcp-proxy.cron-stop", inspect.Config.Labels["lazy-tcp-proxy.cron-stop"])

	httpHealthCheck := types.ParseHTTPHealthCheckLabel(name, inspect.Config.Labels["lazy-tcp-proxy.http-healthcheck"])
	if httpHealthCheck != "" {
		// Log the configured URL, substituting ${container} with the container's
		// current IP address (best effort — may be empty if container is stopped).
		var ip string
		for _, ep := range inspect.NetworkSettings.Networks {
			if ep.IPAddress.IsValid() {
				ip = ep.IPAddress.String()
				break
			}
		}
		var displayURL string
		if ip != "" {
			displayURL = strings.ReplaceAll(httpHealthCheck, "{{container}}", ip)
		} else {
			displayURL = httpHealthCheck // show template when IP not yet assigned
		}
		log.Printf("%s: http-healthcheck URL configured %q", name, displayURL)
	}

	hasHealthCheck := inspect.State.Health != nil &&
		inspect.State.Health.Status != "" &&
		inspect.State.Health.Status != "none"

	https := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.tls"]) == "true"
	apiKey := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.api-key"])

	var traefikHosts []string
	if v := strings.TrimSpace(inspect.Config.Labels["lazy-tcp-proxy.traefik-hosts"]); v != "" {
		traefikHosts = types.ParseTraefikHosts("lazy-tcp-proxy.traefik-hosts", v)
	}

	return types.TargetInfo{
		ContainerID:     containerID,
		ContainerName:   name,
		Ports:           ports,
		UDPPorts:        udpPorts,
		NetworkIDs:      networkIDs,
		AllowList:       allowList,
		BlockList:       blockList,
		IdleTimeout:     idleTimeout,
		StartTimeout:    startTimeout,
		Running:         inspect.State.Running,
		WebhookURL:      webhookURL,
		Dependants:      dependants,
		CronStart:       cronStart,
		CronStop:        cronStop,
		HTTPHealthCheck: httpHealthCheck,
		HasHealthCheck:  hasHealthCheck,
		TLS:             https,
		APIKey:          apiKey,
		TraefikHosts:    traefikHosts,
	}, nil
}

// parseCronLabel validates a cron expression from a container label.
// Returns the expression unchanged if valid, or "" with a warning if invalid.
func parseCronLabel(containerName, labelKey, raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if _, err := cron.ParseStandard(v); err != nil {
		log.Printf("docker: container %s: ignoring invalid %s %q: %v", containerName, labelKey, v, err)
		return ""
	}
	return v
}

// JoinNetworks connects the proxy container to each of the provided network IDs
// if it is not already a member. Returns the names of networks newly joined.
func (m *Manager) JoinNetworks(ctx context.Context, networkIDs []string) ([]string, error) {
	if m.selfID == "" {
		return nil, nil
	}

	var joined []string
	for _, netID := range networkIDs {
		netInfo, err := m.cli.NetworkInspect(ctx, netID, client.NetworkInspectOptions{})
		if err != nil {
			log.Printf("docker: could not inspect network \033[32m%s\033[0m: %v", netID, err)
			continue
		}

		alreadyConnected := false
		for cid := range netInfo.Network.Containers {
			if strings.HasPrefix(cid, m.selfID) || strings.HasPrefix(m.selfID, cid) {
				alreadyConnected = true
				break
			}
		}

		if alreadyConnected {
			continue
		}

		log.Printf("docker: joining network \033[32m%s\033[0m (%s)", netInfo.Network.Name, netID[:12])
		if _, err := m.cli.NetworkConnect(ctx, netID, client.NetworkConnectOptions{Container: m.selfID}); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				log.Printf("docker: failed to join network \033[32m%s\033[0m: %v", netInfo.Network.Name, err)
			}
		} else {
			joined = append(joined, netInfo.Network.Name)
			m.mu.Lock()
			m.joinedNets[netID] = netInfo.Network.Name
			m.mu.Unlock()
		}
	}

	return joined, nil
}

// LeaveNetworks disconnects the proxy container from all networks it joined at runtime.
func (m *Manager) LeaveNetworks(ctx context.Context) {
	if m.selfID == "" {
		return
	}

	m.mu.Lock()
	nets := make(map[string]string, len(m.joinedNets))
	for id, name := range m.joinedNets {
		nets[id] = name
	}
	m.mu.Unlock()

	for id, name := range nets {
		log.Printf("docker: leaving network \033[32m%s\033[0m", name)
		if _, err := m.cli.NetworkDisconnect(ctx, id, client.NetworkDisconnectOptions{Container: m.selfID}); err != nil {
			log.Printf("docker: failed to leave network \033[32m%s\033[0m: %v", name, err)
		}
	}
}

// Shutdown implements the backendManager interface by leaving all joined networks.
func (m *Manager) Shutdown(ctx context.Context) {
	m.LeaveNetworks(ctx)
}

// DefaultTargetID returns name as-is. The Docker API accepts container names
// wherever container IDs are expected, so no lookup is required.
func (m *Manager) DefaultTargetID(name string) string { return name }

// warnSharedDefaultNetworks logs a warning if any of the container's networks is a
// Docker Compose default network that the proxy is already a member of. This situation
// means that running "docker compose down" on the proxy stack will delete the shared
// network and leave the managed container unable to restart.
func (m *Manager) warnSharedDefaultNetworks(ctx context.Context, info types.TargetInfo) {
	if m.selfID == "" {
		return
	}
	for _, netID := range info.NetworkIDs {
		netInfo, err := m.cli.NetworkInspect(ctx, netID, client.NetworkInspectOptions{})
		if err != nil {
			continue
		}
		if netInfo.Network.Labels["com.docker.compose.network"] != "default" {
			continue
		}
		for cid := range netInfo.Network.Containers {
			if strings.HasPrefix(cid, m.selfID) || strings.HasPrefix(m.selfID, cid) {
				log.Printf("docker: WARNING: container %q shares the proxy's default compose network %q.\n"+
					"  Running \"docker compose down\" on the proxy stack will delete this network and leave %q unable to restart.\n"+
					"  Fix: add a top-level \"name:\" field to each of your compose files to give them unique project names.",
					info.ContainerName, netInfo.Network.Name, info.ContainerName)
				break
			}
		}
	}
}

// healthCheckPollInterval is how often WaitUntilHealthy polls the Docker API.
const healthCheckPollInterval = time.Second

// WaitUntilHealthy polls the container's Docker HEALTHCHECK status every
// healthCheckPollInterval until it is "healthy", returns an error immediately
// if "unhealthy", or returns an error once timeout is exceeded.
func (m *Manager) WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error {
	retries := int((timeout + healthCheckPollInterval - 1) / healthCheckPollInterval)
	if retries < 1 {
		retries = 1
	}
	for attempt := 1; attempt <= retries; attempt++ {
		result, err := m.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspecting container: %w", err)
		}
		status := ""
		if result.Container.State.Health != nil {
			status = string(result.Container.State.Health.Status)
		}
		switch status {
		case "healthy":
			log.Printf("proxy: docker-healthcheck: \033[33m%s\033[0m healthy", name)
			return nil
		case "unhealthy":
			return fmt.Errorf("container \033[33m%s\033[0m is unhealthy", name)
		default:
			log.Printf("proxy: docker-healthcheck: attempt %d: \033[33m%s\033[0m → %s", attempt, name, status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(healthCheckPollInterval):
		}
	}
	return fmt.Errorf("container \033[33m%s\033[0m not healthy after %s", name, timeout)
}

// EnsureRunning starts the container (or scales up the swarm service) if not already running.
func (m *Manager) EnsureRunning(ctx context.Context, targetID string) error {
	if desiredReplicas, ok := m.isSwarmService(targetID); ok {
		return m.ensureServiceRunning(ctx, targetID, desiredReplicas)
	}
	result, err := m.cli.ContainerInspect(ctx, targetID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspecting container: %w", err)
	}

	if result.Container.State.Running {
		return nil
	}

	name := strings.TrimPrefix(result.Container.Name, "/")
	log.Printf("docker: starting container \033[33m%s\033[0m", name)
	if _, err := m.cli.ContainerStart(ctx, targetID, client.ContainerStartOptions{}); err != nil {
		if strings.Contains(err.Error(), "network") && strings.Contains(err.Error(), "not found") {
			log.Printf("docker: container %q has a stale network reference; recreate the container to fix this (docker rm %s && docker compose up -d)", name, name)
		}
		return fmt.Errorf("starting container: %w", err)
	}

	log.Printf("docker: container \033[33m%s\033[0m started", name)
	return nil
}

// StopContainer stops the container (or scales the swarm service to 0) with idle-timeout semantics.
func (m *Manager) StopContainer(ctx context.Context, targetID string, targetName string) error {
	if _, ok := m.isSwarmService(targetID); ok {
		return m.stopService(ctx, targetID, targetName)
	}
	timeout := 10
	log.Printf("docker: stopping container \033[33m%s\033[0m (idle timeout)", targetName)
	if _, err := m.cli.ContainerStop(ctx, targetID, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}
	log.Printf("docker: container \033[33m%s\033[0m stopped", targetName)
	return nil
}

// GetUpstreamHost returns the upstream IP for a container or swarm service,
// preferring the given networkID.
func (m *Manager) GetUpstreamHost(ctx context.Context, targetID, preferNetworkID string) (string, error) {
	if _, ok := m.isSwarmService(targetID); ok {
		return m.getServiceUpstreamHost(ctx, targetID, preferNetworkID)
	}
	result, err := m.cli.ContainerInspect(ctx, targetID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}
	inspect := result.Container

	// Try the preferred network first
	if preferNetworkID != "" {
		for _, ep := range inspect.NetworkSettings.Networks {
			if ep.NetworkID == preferNetworkID && ep.IPAddress.IsValid() {
				return ep.IPAddress.String(), nil
			}
		}
	}

	// Fallback: any network with an IP
	for _, ep := range inspect.NetworkSettings.Networks {
		if ep.IPAddress.IsValid() {
			return ep.IPAddress.String(), nil
		}
	}

	return "", fmt.Errorf("no IP address found for container %s", targetID[:12])
}

// isSwarmService reports whether targetID is a registered swarm service.
// Returns (desiredReplicas, true) when found, (0, false) otherwise.
func (m *Manager) isSwarmService(targetID string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.swarmServices[targetID]
	return n, ok
}

func (m *Manager) registerSwarmService(serviceID string, replicas int) {
	m.mu.Lock()
	m.swarmServices[serviceID] = replicas
	m.mu.Unlock()
}

func (m *Manager) unregisterSwarmService(serviceID string) {
	m.mu.Lock()
	delete(m.swarmServices, serviceID)
	m.mu.Unlock()
}

// ensureServiceRunning scales the swarm service to desiredReplicas if currently at 0.
func (m *Manager) ensureServiceRunning(ctx context.Context, serviceID string, desiredReplicas int) error {
	result, err := m.cli.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspecting service: %w", err)
	}
	svc := result.Service
	if svc.Spec.Mode.Replicated == nil {
		return fmt.Errorf("service %s is not a replicated service; only replicated services are supported", svc.Spec.Name)
	}
	if svc.Spec.Mode.Replicated.Replicas != nil && *svc.Spec.Mode.Replicated.Replicas > 0 {
		return nil // already scaled up
	}
	name := svc.Spec.Name
	log.Printf("docker: scaling up service \033[33m%s\033[0m to %d replicas", name, desiredReplicas)
	n := uint64(desiredReplicas)
	svc.Spec.Mode.Replicated.Replicas = &n
	if _, err := m.cli.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
		Version: svc.Meta.Version,
		Spec:    svc.Spec,
	}); err != nil {
		return fmt.Errorf("scaling up service: %w", err)
	}
	log.Printf("docker: service \033[33m%s\033[0m scaled to %d replicas", name, desiredReplicas)
	return nil
}

// stopService scales the swarm service to 0 replicas.
func (m *Manager) stopService(ctx context.Context, serviceID, serviceName string) error {
	result, err := m.cli.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspecting service: %w", err)
	}
	svc := result.Service
	if svc.Spec.Mode.Replicated == nil {
		return fmt.Errorf("service %s is not a replicated service", serviceName)
	}
	if svc.Spec.Mode.Replicated.Replicas != nil && *svc.Spec.Mode.Replicated.Replicas == 0 {
		return nil // already at zero
	}
	log.Printf("docker: scaling down service \033[33m%s\033[0m (idle timeout)", serviceName)
	n := uint64(0)
	svc.Spec.Mode.Replicated.Replicas = &n
	if _, err := m.cli.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
		Version: svc.Meta.Version,
		Spec:    svc.Spec,
	}); err != nil {
		return fmt.Errorf("scaling down service: %w", err)
	}
	log.Printf("docker: service \033[33m%s\033[0m scaled to zero", serviceName)
	return nil
}

// getServiceUpstreamHost returns the VIP of the swarm service on a shared overlay network.
func (m *Manager) getServiceUpstreamHost(ctx context.Context, serviceID, preferNetworkID string) (string, error) {
	result, err := m.cli.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspecting service: %w", err)
	}
	vips := result.Service.Endpoint.VirtualIPs

	// Try the preferred network first
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

	prefix := serviceID
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return "", fmt.Errorf("no VIP found for service %s", prefix)
}

// DiscoverServices lists swarm services labelled lazy-tcp-proxy.enabled=true,
// joins their overlay networks, and calls handler.RegisterTarget for each.
// It is a no-op (with a log) when swarm mode is not active.
func (m *Manager) DiscoverServices(ctx context.Context, handler types.TargetHandler) error {
	infoResult, err := m.cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return fmt.Errorf("getting docker info: %w", err)
	}
	if infoResult.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		log.Printf("docker: swarm mode not active; skipping service discovery")
		return nil
	}

	f := make(client.Filters)
	f.Add("label", "lazy-tcp-proxy.enabled=true")

	svcResult, err := m.cli.ServiceList(ctx, client.ServiceListOptions{Filters: f, Status: true})
	if err != nil {
		return fmt.Errorf("listing services: %w", err)
	}

	var foundNames []string
	var allNetworks []string
	for _, svc := range svcResult.Items {
		info, err := m.serviceToTargetInfo(svc)
		if err != nil {
			log.Printf("docker: discover: skipping service %s: %v", svc.Spec.Name, err)
			continue
		}

		m.registerSwarmService(svc.ID, info.DesiredReplicas)

		joined, err := m.JoinNetworks(ctx, info.NetworkIDs)
		if err != nil {
			log.Printf("docker: discover: failed to join networks for service \033[33m%s\033[0m: %v", info.ContainerName, err)
		}
		allNetworks = append(allNetworks, joined...)

		handler.RegisterTarget(info)
		foundNames = append(foundNames, info.ContainerName)
	}

	if len(foundNames) == 0 {
		log.Printf("docker: init: no proxy services found")
	} else {
		log.Printf("docker: init: found services: \033[33m%s\033[0m", strings.Join(foundNames, ", "))
	}
	if len(allNetworks) > 0 {
		log.Printf("docker: init: joined networks (services): \033[32m%s\033[0m", strings.Join(allNetworks, ", "))
	}

	return nil
}

// serviceToTargetInfo converts a swarm.Service into a TargetInfo.
// Returns an error if the service lacks required labels (scale, ports).
func (m *Manager) serviceToTargetInfo(svc swarm.Service) (types.TargetInfo, error) {
	labels := svc.Spec.Annotations.Labels
	name := svc.Spec.Name

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

	// Network IDs come from the service's VirtualIPs (one per attached overlay network)
	var networkIDs []string
	for _, vip := range svc.Endpoint.VirtualIPs {
		if vip.NetworkID != "" {
			networkIDs = append(networkIDs, vip.NetworkID)
		}
	}

	var allowList, blockList []net.IPNet
	if v, ok := labels["lazy-tcp-proxy.allow-list"]; ok && v != "" {
		allowList = types.ParseIPList("lazy-tcp-proxy.allow-list", v)
	}
	if v, ok := labels["lazy-tcp-proxy.block-list"]; ok && v != "" {
		blockList = types.ParseIPList("lazy-tcp-proxy.block-list", v)
	}

	idleTimeout := types.ParseIdleTimeoutLabel(name, labels["lazy-tcp-proxy.idle-timeout-secs"])
	startTimeout := types.ParseStartTimeoutLabel(name, labels["lazy-tcp-proxy.start-timeout-secs"])

	var webhookURL string
	if v := strings.TrimSpace(labels["lazy-tcp-proxy.webhook-url"]); v != "" {
		if _, err := url.ParseRequestURI(v); err != nil {
			log.Printf("docker: service %s: ignoring invalid webhook URL %q: %v", name, v, err)
		} else {
			webhookURL = v
		}
	}

	var dependants []string
	if v := strings.TrimSpace(labels["lazy-tcp-proxy.dependants"]); v != "" {
		dependants = types.ParseDependants(v)
	}

	cronStart := parseCronLabel(name, "lazy-tcp-proxy.cron-start", labels["lazy-tcp-proxy.cron-start"])
	cronStop := parseCronLabel(name, "lazy-tcp-proxy.cron-stop", labels["lazy-tcp-proxy.cron-stop"])
	httpHealthCheck := types.ParseHTTPHealthCheckLabel(name, labels["lazy-tcp-proxy.http-healthcheck"])

	var traefikHosts []string
	if v := strings.TrimSpace(labels["lazy-tcp-proxy.traefik-hosts"]); v != "" {
		traefikHosts = types.ParseTraefikHosts("lazy-tcp-proxy.traefik-hosts", v)
	}

	running := svc.ServiceStatus != nil && svc.ServiceStatus.RunningTasks > 0

	return types.TargetInfo{
		ContainerID:     svc.ID,
		ContainerName:   name,
		Ports:           ports,
		UDPPorts:        udpPorts,
		NetworkIDs:      networkIDs,
		AllowList:       allowList,
		BlockList:       blockList,
		IdleTimeout:     idleTimeout,
		StartTimeout:    startTimeout,
		Running:         running,
		WebhookURL:      webhookURL,
		Dependants:      dependants,
		CronStart:       cronStart,
		CronStop:        cronStop,
		HTTPHealthCheck: httpHealthCheck,
		HasHealthCheck:  false,
		DesiredReplicas: scale,
		TraefikHosts:    traefikHosts,
	}, nil
}

// NotifyTargets ensures any targets with DesiredReplicas > 0 are registered in
// the swarm services map. Called after config overlay so YAML-only service
// entries are included.
func (m *Manager) NotifyTargets(targets []types.TargetInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, info := range targets {
		if info.DesiredReplicas > 0 {
			m.swarmServices[info.ContainerID] = info.DesiredReplicas
		}
	}
}

// WatchServiceEvents subscribes to Docker swarm service events and notifies
// the handler of create/update/remove actions. Reconnects with exponential
// backoff on error.
func (m *Manager) WatchServiceEvents(ctx context.Context, handler types.TargetHandler) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Skip if swarm is not active (check each reconnect cycle)
		infoResult, err := m.cli.Info(ctx, client.InfoOptions{})
		if err != nil || infoResult.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
			return // swarm not available; stop the goroutine silently
		}

		f := make(client.Filters)
		f.Add("type", string(events.ServiceEventType))
		f.Add("event", "create")
		f.Add("event", "update")
		f.Add("event", "remove")

		eventsResult := m.cli.Events(ctx, client.EventsListOptions{Filters: f})

		log.Printf("docker: watching service events...")
		eventLoop := true
		for eventLoop {
			select {
			case <-ctx.Done():
				return
			case err := <-eventsResult.Err:
				if err != nil {
					log.Printf("docker: service events error: %v; reconnecting in %s", err, backoff)
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					eventLoop = false
				}
			case msg := <-eventsResult.Messages:
				backoff = time.Second
				m.handleServiceEvent(ctx, msg, handler)
			}
		}
	}
}

// handleServiceEvent processes a single swarm service event message.
func (m *Manager) handleServiceEvent(ctx context.Context, msg events.Message, handler types.TargetHandler) {
	svcName := msg.Actor.Attributes["name"]

	switch msg.Action {
	case "create":
		if msg.Actor.Attributes["lazy-tcp-proxy.enabled"] != "true" {
			log.Printf("docker: service event: service %s created but not proxied: missing label lazy-tcp-proxy.enabled=true", svcName)
			return
		}
		result, err := m.cli.ServiceInspect(ctx, msg.Actor.ID, client.ServiceInspectOptions{InsertDefaults: true})
		if err != nil {
			log.Printf("docker: service event: could not inspect service %s: %v", svcName, err)
			return
		}
		info, err := m.serviceToTargetInfo(result.Service)
		if err != nil {
			log.Printf("docker: service event: skipping service %s: %v", svcName, err)
			return
		}
		log.Printf("docker: service event: service added: \033[33m%s\033[0m", svcName)
		m.registerSwarmService(info.ContainerID, info.DesiredReplicas)
		joined, err := m.JoinNetworks(ctx, info.NetworkIDs)
		if err != nil {
			log.Printf("docker: service event: failed to join networks for \033[33m%s\033[0m: %v", svcName, err)
		}
		for _, n := range joined {
			log.Printf("docker: service event: joined network: \033[32m%s\033[0m", n)
		}
		handler.RegisterTarget(info)

	case "update":
		if _, known := m.isSwarmService(msg.Actor.ID); !known {
			return // not a service we manage
		}
		result, err := m.cli.ServiceInspect(ctx, msg.Actor.ID, client.ServiceInspectOptions{InsertDefaults: true})
		if err != nil {
			log.Printf("docker: service event: could not inspect service %s: %v", svcName, err)
			return
		}
		svc := result.Service
		var desiredReplicas uint64
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			desiredReplicas = *svc.Spec.Mode.Replicated.Replicas
		}
		var runningTasks uint64
		if svc.ServiceStatus != nil {
			runningTasks = svc.ServiceStatus.RunningTasks
		}
		if desiredReplicas == 0 && runningTasks == 0 {
			log.Printf("docker: service event: service stopped externally: \033[33m%s\033[0m (still registered)", svcName)
			handler.ContainerStopped(msg.Actor.ID)
		}

	case "remove":
		if _, known := m.isSwarmService(msg.Actor.ID); !known {
			return
		}
		log.Printf("docker: service event: service removed: \033[33m%s\033[0m", svcName)
		m.unregisterSwarmService(msg.Actor.ID)
		handler.RemoveTarget(msg.Actor.ID)
	}
}

// WatchEvents subscribes to Docker events for containers with the proxy label.
// On create/start events it calls handler.RegisterTarget; on die/destroy events
// it calls handler.ContainerStopped/RemoveTarget.
// It reconnects with exponential backoff on error.
func (m *Manager) WatchEvents(ctx context.Context, handler types.TargetHandler) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		f := make(client.Filters)
		f.Add("type", string(events.ContainerEventType))
		f.Add("event", "start")
		f.Add("event", "die")
		f.Add("event", "destroy")
		f.Add("event", "create")

		eventsResult := m.cli.Events(ctx, client.EventsListOptions{Filters: f})

		log.Printf("docker: watching events...")
		eventLoop := true
		for eventLoop {
			select {
			case <-ctx.Done():
				return
			case err := <-eventsResult.Err:
				if err != nil {
					log.Printf("docker: events error: %v; reconnecting in %s", err, backoff)
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					eventLoop = false
				}
			case msg := <-eventsResult.Messages:
				backoff = time.Second // reset on success
				switch msg.Action {
				case "create", "start":
					name := msg.Actor.Attributes["name"]
					attrs := msg.Actor.Attributes
					if attrs["lazy-tcp-proxy.enabled"] != "true" {
						log.Printf("docker: event: container %s started but not proxied: missing label lazy-tcp-proxy.enabled=true", name)
						continue
					}
					portsVal, hasPorts := attrs["lazy-tcp-proxy.ports"]
					udpPortsVal := attrs["lazy-tcp-proxy.udp-ports"]
					if !hasPorts && udpPortsVal == "" {
						log.Printf("docker: event: container %s started but not proxied: missing label lazy-tcp-proxy.ports or lazy-tcp-proxy.udp-ports", name)
						continue
					}
					valid := !hasPorts // UDP-only: skip TCP validation
					if hasPorts {
						for _, token := range strings.Split(portsVal, ",") {
							parts := strings.SplitN(strings.TrimSpace(token), ":", 2)
							if len(parts) == 2 {
								_, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
								_, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
								if e1 == nil && e2 == nil {
									valid = true
									break
								}
							}
						}
					}
					if !valid {
						log.Printf("docker: event: container %s started but not proxied: invalid ports value %q", name, portsVal)
						continue
					}
					log.Printf("docker: event: container added: \033[33m%s\033[0m", name)
					info, err := m.containerToTargetInfo(ctx, msg.Actor.ID)
					if err != nil {
						log.Printf("docker: event: could not get target info for %s: %v", name, err)
						continue
					}
					m.warnSharedDefaultNetworks(ctx, info)
					joined, err := m.JoinNetworks(ctx, info.NetworkIDs)
					if err != nil {
						log.Printf("docker: event: failed to join networks: %v", err)
					}
					for _, n := range joined {
						log.Printf("docker: event: joined network: \033[32m%s\033[0m", n)
					}
					handler.RegisterTarget(info)
					if msg.Action == "start" {
						handler.ContainerStarted(msg.Actor.ID)
					}

				case "die":
					name := msg.Actor.Attributes["name"]
					log.Printf("docker: event: container stopped: \033[33m%s\033[0m (still registered)", name)
					handler.ContainerStopped(msg.Actor.ID)

				case "destroy":
					name := msg.Actor.Attributes["name"]
					log.Printf("docker: event: container removed: \033[33m%s\033[0m", name)
					handler.RemoveTarget(msg.Actor.ID)
				}
			}
		}
	}
}
