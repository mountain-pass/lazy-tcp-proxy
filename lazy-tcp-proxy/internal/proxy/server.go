package proxy

import (
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

const (
	dialInterval = time.Second
	copyBufSize  = 32 * 1024
)

// TargetSnapshot is a point-in-time copy of a single port mapping's state,
// safe to read without holding any lock.
type TargetSnapshot struct {
	ContainerID        string     `json:"container_id"`
	ContainerName      string     `json:"container_name"`
	ListenPort         int        `json:"listen_port"`
	TargetPort         int        `json:"target_port"`
	Running            bool       `json:"running"`
	ActiveConns        int32      `json:"active_conns"`
	LastActive         *time.Time `json:"last_active"`
	LastActiveRelative string     `json:"last_active_relative"`
}

// relativeTime returns a human-readable string describing how long ago t was,
// using only the single largest significant unit.
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d >= 365*24*time.Hour:
		return fmt.Sprintf("%d years ago", int(d.Hours()/24/365))
	case d >= 30*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/24/30))
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	}
}

// effectiveTimeout returns the per-container idle timeout if set, otherwise the server default.
func effectiveTimeout(perContainer *time.Duration, global time.Duration) time.Duration {
	if perContainer != nil {
		return *perContainer
	}
	return global
}

// countingWriter wraps an io.Writer and accumulates bytes written.
// Not safe for concurrent use; each goroutine must use its own instance.
type countingWriter struct {
	w io.Writer
	n *int64
}

func (cw countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	*cw.n += int64(n)
	return n, err
}

var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, copyBufSize)
		return &b
	},
}

// dependantState tracks running state for containers registered with no proxy ports.
// These containers participate only in cascade start/stop.
type dependantState struct {
	containerName string
	running       bool
}

// targetState holds runtime state for a single listen-port→container-port mapping.
type targetState struct {
	info            types.TargetInfo
	listenPort      int
	targetPort      int
	listener        net.Listener
	lastActive      time.Time
	activeConns     atomic.Int32
	idleTimeout     *time.Duration // nil = use server default
	startTimeout    time.Duration  // how long to retry dialing the upstream on cold start
	httpHealthCheck string         // URL to poll for readiness; "" = disabled
	hasHealthCheck  bool           // true if the container has a Docker HEALTHCHECK configured
	running         bool
	removed         bool
}

// webhookPayload is the JSON body sent to a container's webhook URL.
type webhookPayload struct {
	Event         string `json:"event"`
	ConnectionID  string `json:"connection_id,omitempty"`
	RemoteAddr    string `json:"remote_addr,omitempty"`
	RemotePort    int    `json:"remote_port,omitempty"`
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Timestamp     string `json:"timestamp"`
	ListenPort    int    `json:"listen_port,omitempty"`
	UpstreamAddr  string `json:"upstream_addr,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	EndedAt       string `json:"ended_at,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	BytesSent     int64  `json:"bytes_sent,omitempty"`
	BytesReceived int64  `json:"bytes_received,omitempty"`
}

// newConnectionID returns a random UUID v4 string.
func newConnectionID() string {
	var b [16]byte
	if _, err := crypto_rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:]))
}

// containerBackend is the subset of backend methods used by ProxyServer.
type containerBackend interface {
	EnsureRunning(ctx context.Context, targetID string) error
	StopContainer(ctx context.Context, targetID, targetName string) error
	GetUpstreamHost(ctx context.Context, targetID, hint string) (string, error)
	WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error
}

// cronScheduler is the subset of scheduler methods used by ProxyServer.
// Defined as an interface so proxy does not import the scheduler package.
type cronScheduler interface {
	Register(info types.TargetInfo)
	Unregister(targetID string)
}

// ProxyServer manages TCP listeners and proxies connections to targets.
type ProxyServer struct {
	backend       containerBackend
	ctx           context.Context
	mu            sync.RWMutex
	targets       map[int]*targetState     // keyed by TCP listen port
	udpTargets    map[int]*udpListenerState // keyed by UDP listen port
	nameToID        map[string]string        // ContainerName → ContainerID for cascade lookup
	dependantStates map[string]*dependantState // containerID → state for port-less containers
	pollInterval  time.Duration
	idleTimeout   time.Duration
	startTimeout  time.Duration
	startTime     time.Time
	webhookClient *http.Client
	sched         cronScheduler      // nil if no scheduler configured
	startGroup    singleflight.Group // deduplicates concurrent EnsureRunning calls per container
}

// NewServer creates a new ProxyServer backed by the given backend.
func NewServer(ctx context.Context, b containerBackend, startTime time.Time, idleTimeout, pollInterval, startTimeout time.Duration) *ProxyServer {
	return &ProxyServer{
		backend:       b,
		ctx:           ctx,
		targets:         make(map[int]*targetState),
		udpTargets:      make(map[int]*udpListenerState),
		nameToID:        make(map[string]string),
		dependantStates: make(map[string]*dependantState),
		idleTimeout:   idleTimeout,
		startTimeout:  startTimeout,
		pollInterval:  pollInterval,
		startTime:     startTime,
		webhookClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// SetScheduler injects the cron scheduler. Must be called before Discover.
func (s *ProxyServer) SetScheduler(sched cronScheduler) {
	s.sched = sched
}

// CronStart is called by the scheduler to start a target on its cron-start schedule.
// It is a no-op (with a log) if the target is already running.
func (s *ProxyServer) CronStart(ctx context.Context, targetID, targetName string) {
	s.mu.RLock()
	running, found := s.isRunning(targetID)
	s.mu.RUnlock()
	if !found {
		log.Printf("scheduler: cron-start: \033[33m%s\033[0m not found, skipping", targetName)
		return
	}
	if running {
		log.Printf("scheduler: cron-start: \033[33m%s\033[0m already running, no action", targetName)
		return
	}
	if err := s.backend.EnsureRunning(ctx, targetID); err != nil {
		log.Printf("scheduler: cron-start: failed to start \033[33m%s\033[0m: %v", targetName, err)
		return
	}
	s.mu.Lock()
	for _, ts := range s.targets {
		if ts.info.ContainerID == targetID {
			ts.running = true
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == targetID {
			uls.running = true
		}
	}
	s.mu.Unlock()
	log.Printf("scheduler: cron-start: started \033[33m%s\033[0m", targetName)

	s.mu.RLock()
	var info types.TargetInfo
	for _, ts := range s.targets {
		if ts.info.ContainerID == targetID {
			info = ts.info
			break
		}
	}
	s.mu.RUnlock()
	if info.WebhookURL != "" {
		go s.fireWebhook(info.WebhookURL, webhookPayload{Event: "container_started", ContainerID: targetID, ContainerName: targetName})
	}
	if len(info.Dependants) > 0 {
		go s.cascadeStart(info)
	}
}

// CronStop is called by the scheduler to stop a target on its cron-stop schedule.
// It is a no-op (with a log) if the target is already stopped.
func (s *ProxyServer) CronStop(ctx context.Context, targetID, targetName string) {
	s.mu.RLock()
	running, found := s.isRunning(targetID)
	s.mu.RUnlock()
	if !found {
		log.Printf("scheduler: cron-stop: \033[33m%s\033[0m not found, skipping", targetName)
		return
	}
	if !running {
		log.Printf("scheduler: cron-stop: \033[33m%s\033[0m already stopped, no action", targetName)
		return
	}
	if err := s.backend.StopContainer(ctx, targetID, targetName); err != nil {
		log.Printf("scheduler: cron-stop: failed to stop \033[33m%s\033[0m: %v", targetName, err)
		return
	}
	s.mu.Lock()
	var info types.TargetInfo
	for _, ts := range s.targets {
		if ts.info.ContainerID == targetID {
			ts.running = false
			info = ts.info
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == targetID {
			uls.running = false
			info = uls.info
		}
	}
	s.mu.Unlock()
	log.Printf("scheduler: cron-stop: stopped \033[33m%s\033[0m", targetName)
	if info.WebhookURL != "" {
		go s.fireWebhook(info.WebhookURL, webhookPayload{Event: "container_stopped", ContainerID: targetID, ContainerName: targetName})
	}
	if len(info.Dependants) > 0 {
		go s.cascadeStop(info)
	}
}

// isRunning reports whether any mapping for targetID is running.
// Caller must hold s.mu (at least RLock).
func (s *ProxyServer) isRunning(targetID string) (running, found bool) {
	for _, ts := range s.targets {
		if ts.info.ContainerID == targetID {
			found = true
			if ts.running {
				running = true
			}
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == targetID {
			found = true
			if uls.running {
				running = true
			}
		}
	}
	return
}

// Snapshot returns a point-in-time copy of all registered targets.
func (s *ProxyServer) Snapshot() []TargetSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]TargetSnapshot, 0, len(s.targets))
	for listenPort, ts := range s.targets {
		effective := ts.lastActive
		if effective.IsZero() {
			effective = s.startTime
		}
		t := effective
		id := ts.info.ContainerID
		if len(id) > 12 {
			id = id[:12]
		}
		out = append(out, TargetSnapshot{
			ContainerID:        id,
			ContainerName:      ts.info.ContainerName,
			ListenPort:         listenPort,
			TargetPort:         ts.targetPort,
			Running:            ts.running,
			ActiveConns:        ts.activeConns.Load(),
			LastActive:         &t,
			LastActiveRelative: relativeTime(effective, now),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContainerName != out[j].ContainerName {
			return out[i].ContainerName < out[j].ContainerName
		}
		return out[i].ContainerID < out[j].ContainerID
	})
	return out
}

// fireWebhook POSTs a lifecycle event to the container's webhook URL.
// The caller populates all fields of payload except Timestamp (set here)
// and ContainerID truncation (applied here). Must be called in a goroutine.
func (s *ProxyServer) fireWebhook(webhookURL string, payload webhookPayload) {
	if len(payload.ContainerID) > 12 {
		payload.ContainerID = payload.ContainerID[:12]
	}
	payload.Timestamp = time.Now().UTC().Format(time.RFC3339)
	body, _ := json.Marshal(payload)
	resp, err := s.webhookClient.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("proxy: webhook: POST %s event=%s error: %v", webhookURL, payload.Event, err)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("proxy: webhook: POST %s event=%s non-2xx response: %d", webhookURL, payload.Event, resp.StatusCode)
		return
	}
	log.Printf("proxy: webhook: delivered event=%s to %s (%d)", payload.Event, webhookURL, resp.StatusCode)
}

// RegisterTarget adds or updates a target. One listener is created per port mapping.
func (s *ProxyServer) RegisterTarget(info types.TargetInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Pre-flight: reject the entire registration if any declared TCP or UDP port
	// is already held by a different container.
	for _, m := range info.Ports {
		if existing, ok := s.targets[m.ListenPort]; ok && existing.info.ContainerID != info.ContainerID {
			log.Printf("\033[31mproxy: TCP port conflict on port %d: already registered by \033[33m%s\033[31m, ignoring \033[33m%s\033[31m\033[0m",
				m.ListenPort, existing.info.ContainerName, info.ContainerName)
			return
		}
	}
	for _, m := range info.UDPPorts {
		if existing, ok := s.udpTargets[m.ListenPort]; ok && existing.info.ContainerID != info.ContainerID {
			log.Printf("\033[31mproxy: UDP port conflict on port %d: already registered by \033[33m%s\033[31m, ignoring \033[33m%s\033[31m\033[0m",
				m.ListenPort, existing.info.ContainerName, info.ContainerName)
			return
		}
	}

	// Register TCP listeners.
	for _, m := range info.Ports {
		if existing, ok := s.targets[m.ListenPort]; ok {
			existing.info = info
			existing.targetPort = m.TargetPort
			existing.idleTimeout = info.IdleTimeout
			existing.startTimeout = effectiveTimeout(info.StartTimeout, s.startTimeout)
			existing.httpHealthCheck = info.HTTPHealthCheck
			existing.hasHealthCheck = info.HasHealthCheck
			existing.running = info.Running
			existing.removed = false
			log.Printf("proxy: updated TCP target \033[33m%s\033[0m on port %d->%d", info.ContainerName, m.ListenPort, m.TargetPort)
			continue
		}

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", m.ListenPort))
		if err != nil {
			log.Printf("proxy: failed to listen on TCP port %d for \033[33m%s\033[0m: %v", m.ListenPort, info.ContainerName, err)
			continue
		}

		ts := &targetState{
			info:            info,
			listenPort:      m.ListenPort,
			targetPort:      m.TargetPort,
			listener:        ln,
			lastActive:      time.Time{}, // zero — immediately idle
			idleTimeout:     info.IdleTimeout,
			startTimeout:    effectiveTimeout(info.StartTimeout, s.startTimeout),
			httpHealthCheck: info.HTTPHealthCheck,
			hasHealthCheck:  info.HasHealthCheck,
			running:         info.Running,
		}
		s.targets[m.ListenPort] = ts
		log.Printf("proxy: registered target \033[33m%s\033[0m, TCP %d->%d", info.ContainerName, m.ListenPort, m.TargetPort)
		go s.acceptLoop(ts)
	}

	// Register UDP listeners.
	for _, m := range info.UDPPorts {
		if existing, ok := s.udpTargets[m.ListenPort]; ok {
			existing.info = info
			existing.targetPort = m.TargetPort
			existing.idleTimeout = info.IdleTimeout
			existing.startTimeout = effectiveTimeout(info.StartTimeout, s.startTimeout)
			existing.running = info.Running
			existing.removed = false
			log.Printf("proxy: updated UDP target \033[33m%s\033[0m on port %d->%d", info.ContainerName, m.ListenPort, m.TargetPort)
			continue
		}

		pc, err := net.ListenPacket("udp", fmt.Sprintf(":%d", m.ListenPort))
		if err != nil {
			log.Printf("proxy: failed to listen on UDP port %d for \033[33m%s\033[0m: %v", m.ListenPort, info.ContainerName, err)
			continue
		}
		uls := &udpListenerState{
			listenConn:   pc.(*net.UDPConn),
			listenPort:   m.ListenPort,
			targetPort:   m.TargetPort,
			info:         info,
			idleTimeout:  info.IdleTimeout,
			startTimeout: effectiveTimeout(info.StartTimeout, s.startTimeout),
			running:      info.Running,
			flows:        make(map[string]*udpFlow),
			pending:      make(map[string]bool),
		}
		uls.upstreamReadyCond = sync.NewCond(&uls.mu)
		s.udpTargets[m.ListenPort] = uls
		log.Printf("proxy: registered target \033[33m%s\033[0m, UDP %d->%d", info.ContainerName, m.ListenPort, m.TargetPort)
		go s.udpReadLoop(uls)
		go s.udpFlowSweeper(s.ctx, uls, s.pollInterval)
	}

	// Keep name→ID map current for cascade lookups.
	s.nameToID[info.ContainerName] = info.ContainerID

	// Port-less containers: track running state in dependantStates; no listeners opened.
	if len(info.Ports) == 0 && len(info.UDPPorts) == 0 {
		if existing, ok := s.dependantStates[info.ContainerID]; ok {
			existing.containerName = info.ContainerName
			existing.running = info.Running
			log.Printf("proxy: updated target \033[33m%s\033[0m (cascade-only, no ports)", info.ContainerName)
		} else {
			s.dependantStates[info.ContainerID] = &dependantState{
				containerName: info.ContainerName,
				running:       info.Running,
			}
			log.Printf("proxy: registered target \033[33m%s\033[0m (cascade-only, no ports)", info.ContainerName)
		}
	}

	// Register with cron scheduler if any schedule is set.
	if s.sched != nil && (info.CronStart != "" || info.CronStop != "") {
		s.sched.Register(info)
	}
}

// RemoveTarget closes and removes all listeners for the given container.
func (s *ProxyServer) RemoveTarget(containerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for port, ts := range s.targets {
		if ts.info.ContainerID == containerID {
			log.Printf("proxy: removing target \033[33m%s\033[0m on TCP port %d", ts.info.ContainerName, port)
			delete(s.nameToID, ts.info.ContainerName)
			ts.removed = true
			if err := ts.listener.Close(); err != nil {
				log.Printf("proxy: error closing TCP listener on port %d: %v", port, err)
			}
			delete(s.targets, port)
		}
	}
	for port, uls := range s.udpTargets {
		if uls.info.ContainerID == containerID {
			log.Printf("proxy: removing target \033[33m%s\033[0m on UDP port %d", uls.info.ContainerName, port)
			delete(s.nameToID, uls.info.ContainerName)
			uls.removed = true
			if err := uls.listenConn.Close(); err != nil {
				log.Printf("proxy: error closing UDP listener on port %d: %v", port, err)
			}
			delete(s.udpTargets, port)
		}
	}
	if ds, ok := s.dependantStates[containerID]; ok {
		log.Printf("proxy: removing target \033[33m%s\033[0m (cascade-only)", ds.containerName)
		delete(s.nameToID, ds.containerName)
		delete(s.dependantStates, containerID)
	}
	if s.sched != nil {
		s.sched.Unregister(containerID)
	}
}

// ContainerStopped marks all port mappings for the given container as stopped
// and cascades a stop to any declared dependants.
func (s *ProxyServer) ContainerStopped(containerID string) {
	s.mu.RLock()
	var info types.TargetInfo
	var affectedULS []*udpListenerState
	for _, ts := range s.targets {
		if ts.info.ContainerID == containerID {
			ts.running = false
			info = ts.info
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == containerID {
			uls.running = false
			info = uls.info
			affectedULS = append(affectedULS, uls)
		}
	}
	if ds, ok := s.dependantStates[containerID]; ok {
		ds.running = false
	}
	s.mu.RUnlock()
	// Reset upstream readiness state so the next cold start re-probes.
	// If the container stopped externally while a retry loop was in progress,
	// also wake any goroutines blocked on the shared wait.
	for _, uls := range affectedULS {
		uls.mu.Lock()
		uls.upstreamReady = false
		if uls.upstreamStarting {
			uls.upstreamStarting = false
			uls.upstreamReadyCond.Broadcast()
		}
		uls.mu.Unlock()
	}
	if len(info.Dependants) > 0 {
		go s.cascadeStop(info)
	}
}

// ContainerStarted cascades a start to any declared dependants.
func (s *ProxyServer) ContainerStarted(containerID string) {
	s.mu.RLock()
	var info types.TargetInfo
	for _, ts := range s.targets {
		if ts.info.ContainerID == containerID {
			info = ts.info
			break
		}
	}
	if ds, ok := s.dependantStates[containerID]; ok {
		ds.running = true
	}
	s.mu.RUnlock()
	if len(info.Dependants) > 0 {
		go s.cascadeStart(info)
	}
}

// RunInactivityChecker periodically stops idle containers.
func (s *ProxyServer) RunInactivityChecker(ctx context.Context, tick time.Duration) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkInactivity(ctx)
		}
	}
}

func (s *ProxyServer) checkInactivity(ctx context.Context) {
	s.mu.RLock()
	tcpSnap := make([]*targetState, 0, len(s.targets))
	for _, ts := range s.targets {
		tcpSnap = append(tcpSnap, ts)
	}
	udpSnap := make([]*udpListenerState, 0, len(s.udpTargets))
	for _, uls := range s.udpTargets {
		udpSnap = append(udpSnap, uls)
	}
	s.mu.RUnlock()

	// Group by container; eligible only when ALL TCP and UDP mappings are idle.
	type entry struct {
		containerID string
		name        string
		webhookURL  string
		info        types.TargetInfo
		allIdle     bool
		tcpStates   []*targetState
		udpStates   []*udpListenerState
	}
	byContainer := map[string]*entry{}

	for _, ts := range tcpSnap {
		if ts.removed {
			continue
		}
		if ts.info.CronStart != "" || ts.info.CronStop != "" {
			continue // lifecycle managed by cron scheduler
		}
		e, ok := byContainer[ts.info.ContainerID]
		if !ok {
			e = &entry{containerID: ts.info.ContainerID, name: ts.info.ContainerName, webhookURL: ts.info.WebhookURL, info: ts.info, allIdle: true}
			byContainer[ts.info.ContainerID] = e
		}
		e.tcpStates = append(e.tcpStates, ts)
		eff := effectiveTimeout(ts.idleTimeout, s.idleTimeout)
		if !ts.running || ts.activeConns.Load() > 0 || time.Since(ts.lastActive) < eff {
			e.allIdle = false
		}
	}

	for _, uls := range udpSnap {
		if uls.removed {
			continue
		}
		if uls.info.CronStart != "" || uls.info.CronStop != "" {
			continue // lifecycle managed by cron scheduler
		}
		e, ok := byContainer[uls.info.ContainerID]
		if !ok {
			e = &entry{containerID: uls.info.ContainerID, name: uls.info.ContainerName, webhookURL: uls.info.WebhookURL, info: uls.info, allIdle: true}
			byContainer[uls.info.ContainerID] = e
		}
		e.udpStates = append(e.udpStates, uls)
		uls.mu.Lock()
		activeFlows := len(uls.flows) + len(uls.pending)
		lastActive := uls.lastActive
		uls.mu.Unlock()
		eff := effectiveTimeout(uls.idleTimeout, s.idleTimeout)
		if !uls.running || activeFlows > 0 || time.Since(lastActive) < eff {
			e.allIdle = false
		}
	}

	for _, e := range byContainer {
		if e.allIdle {
			if err := s.backend.StopContainer(ctx, e.containerID, e.name); err != nil {
				log.Printf("proxy: inactivity: error stopping \033[33m%s\033[0m: %v", e.name, err)
			} else {
				for _, ts := range e.tcpStates {
					ts.running = false
				}
				for _, uls := range e.udpStates {
					uls.running = false
				}
				if e.webhookURL != "" {
					go s.fireWebhook(e.webhookURL, webhookPayload{Event: "container_stopped", ContainerID: e.containerID, ContainerName: e.name})
				}
				if len(e.info.Dependants) > 0 {
					go s.cascadeStop(e.info)
				}
			}
		}
	}
}

// ipBlocked returns true if the remote address should be denied based on the
// target's allow-list and block-list.
func ipBlocked(remoteAddr string, info types.TargetInfo) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if len(info.AllowList) > 0 {
		allowed := false
		for _, n := range info.AllowList {
			if n.Contains(ip) {
				allowed = true
				break
			}
		}
		if !allowed {
			return true
		}
	}
	for _, n := range info.BlockList {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// resolveHealthURL substitutes {{container}} in the health-check URL template
// with the upstream IP/host returned by GetUpstreamHost. Returns the resolved
// URL and nil on success. If {{container}} is present but the IP cannot be
// determined, returns ("", error) so the caller can skip the health check.
// URLs without {{container}} are returned unchanged.
func (s *ProxyServer) resolveHealthURL(ctx context.Context, rawURL, containerID, containerName, hint string) (string, error) {
	if !strings.Contains(rawURL, "{{container}}") {
		return rawURL, nil
	}
	host, err := s.backend.GetUpstreamHost(ctx, containerID, hint)
	if err != nil {
		return "", fmt.Errorf("cannot resolve IP for \033[33m%s\033[0m: %w", containerName, err)
	}
	if host == "" {
		return "", fmt.Errorf("cannot resolve IP for \033[33m%s\033[0m: no address available", containerName)
	}
	return strings.ReplaceAll(rawURL, "{{container}}", host), nil
}

// waitForHTTPReady polls url with HTTP GET every dialInterval until a 2xx
// response is received or the timeout is exceeded. Returns nil on success.
func (s *ProxyServer) waitForHTTPReady(ctx context.Context, url, name string, timeout time.Duration) error {
	retries := int((timeout + dialInterval - 1) / dialInterval)
	if retries < 1 {
		retries = 1
	}
	for attempt := 1; attempt <= retries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("building http-healthcheck request: %w", err)
		}
		resp, err := s.webhookClient.Do(req)
		if err == nil {
			resp.Body.Close() //nolint:errcheck
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				log.Printf("proxy: http-healthcheck: \033[33m%s\033[0m ready (%d)", name, resp.StatusCode)
				return nil
			}
			log.Printf("proxy: http-healthcheck: attempt %d: \033[33m%s\033[0m → %d", attempt, name, resp.StatusCode)
		} else {
			log.Printf("proxy: http-healthcheck: attempt %d: \033[33m%s\033[0m → %v", attempt, name, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dialInterval):
		}
	}
	return fmt.Errorf("upstream \033[33m%s\033[0m not ready after %s", name, timeout)
}

// acceptLoop runs in a goroutine for each target listener.
func (s *ProxyServer) acceptLoop(ts *targetState) {
	for {
		conn, err := ts.listener.Accept()
		if err != nil {
			if ts.removed {
				return
			}
			log.Printf("proxy: accept error on port %d: %v", ts.targetPort, err)
			return
		}
		go s.handleConn(conn, ts)
	}
}

// handleConn manages a single inbound connection to a target.
func (s *ProxyServer) handleConn(conn net.Conn, ts *targetState) {
	defer conn.Close() //nolint:errcheck

	ts.activeConns.Add(1)
	defer func() {
		if ts.activeConns.Add(-1) == 0 {
			eff := effectiveTimeout(ts.idleTimeout, s.idleTimeout)
			if eff == 0 {
				log.Printf("proxy: last connection to \033[33m%s\033[0m closed; idle timer started (container will stop immediately if no new connections)",
					ts.info.ContainerName)
			} else {
				log.Printf("proxy: last connection to \033[33m%s\033[0m closed; idle timer started (container will stop in ~%s if no new connections)",
					ts.info.ContainerName, eff)
			}
			go debug.FreeOSMemory()
		}
	}()

	ctx := context.Background()

	if ipBlocked(conn.RemoteAddr().String(), ts.info) {
		log.Printf("proxy: new connection to \033[33m%s\033[0m (port %d) from \033[36m%s\033[0m \033[31m(blocked)\033[0m",
			ts.info.ContainerName, ts.targetPort, conn.RemoteAddr())
		return
	}
	log.Printf("proxy: new connection to \033[33m%s\033[0m (port %d) from \033[36m%s\033[0m",
		ts.info.ContainerName, ts.targetPort, conn.RemoteAddr())

	remoteIP, remotePortStr, _ := net.SplitHostPort(conn.RemoteAddr().String())
	remotePort, _ := strconv.Atoi(remotePortStr)
	connID := newConnectionID()
	startedAt := time.Now()
	var upstreamAddr string
	var bytesSent, bytesReceived int64
	if ts.info.WebhookURL != "" {
		go s.fireWebhook(ts.info.WebhookURL, webhookPayload{
			Event:         "tcp_conn_start",
			ConnectionID:  connID,
			RemoteAddr:    remoteIP,
			RemotePort:    remotePort,
			ContainerID:   ts.info.ContainerID,
			ContainerName: ts.info.ContainerName,
			ListenPort:    ts.listenPort,
			StartedAt:     startedAt.UTC().Format(time.RFC3339),
		})
		defer func() {
			endedAt := time.Now()
			go s.fireWebhook(ts.info.WebhookURL, webhookPayload{
				Event:         "tcp_conn_end",
				ConnectionID:  connID,
				RemoteAddr:    remoteIP,
				RemotePort:    remotePort,
				ContainerID:   ts.info.ContainerID,
				ContainerName: ts.info.ContainerName,
				ListenPort:    ts.listenPort,
				UpstreamAddr:  upstreamAddr,
				StartedAt:     startedAt.UTC().Format(time.RFC3339),
				EndedAt:       endedAt.UTC().Format(time.RFC3339),
				DurationMs:    endedAt.Sub(startedAt).Milliseconds(),
				BytesSent:     bytesSent,
				BytesReceived: bytesReceived,
			})
		}()
	}

	_, startErr, shared := s.startGroup.Do(ts.info.ContainerID, func() (any, error) {
		return nil, s.backend.EnsureRunning(ctx, ts.info.ContainerID)
	})
	if shared {
		log.Printf("proxy: joined in-flight startup for \033[33m%s\033[0m", ts.info.ContainerName)
	}
	if startErr != nil {
		log.Printf("proxy: could not start container \033[33m%s\033[0m: %v", ts.info.ContainerName, startErr)
		return
	}
	if ts.info.WebhookURL != "" {
		go s.fireWebhook(ts.info.WebhookURL, webhookPayload{Event: "container_started", ContainerID: ts.info.ContainerID, ContainerName: ts.info.ContainerName})
	}

	// Determine preferred network hint (first network ID in list; unused in k8s mode)
	var hint string
	if len(ts.info.NetworkIDs) > 0 {
		hint = ts.info.NetworkIDs[0]
	}

	if ts.httpHealthCheck != "" {
		healthURL, err := s.resolveHealthURL(ctx, ts.httpHealthCheck, ts.info.ContainerID, ts.info.ContainerName, hint)
		if err != nil {
			log.Printf("proxy: http-healthcheck: %v; skipping health check", err)
		} else if err := s.waitForHTTPReady(ctx, healthURL, ts.info.ContainerName, ts.startTimeout); err != nil {
			log.Printf("proxy: http-healthcheck: %v; dropping connection from \033[36m%s\033[0m", err, conn.RemoteAddr())
			return
		}
	} else if ts.hasHealthCheck {
		if err := s.backend.WaitUntilHealthy(ctx, ts.info.ContainerID, ts.info.ContainerName, ts.startTimeout); err != nil {
			log.Printf("proxy: docker-healthcheck: %v; dropping connection from \033[36m%s\033[0m", err, conn.RemoteAddr())
			return
		}
	}

	// Retry dial to upstream — budget derived from the configured start timeout.
	retries := int((ts.startTimeout + dialInterval - 1) / dialInterval)
	if retries < 1 {
		retries = 1
	}
	var upstream net.Conn
	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		host, err := s.backend.GetUpstreamHost(ctx, ts.info.ContainerID, hint)
		if err != nil {
			log.Printf("proxy: attempt %d: could not get upstream host for \033[33m%s\033[0m: %v", attempt, ts.info.ContainerName, err)
			time.Sleep(dialInterval)
			continue
		}

		addr := net.JoinHostPort(host, fmt.Sprintf("%d", ts.targetPort))
		upstream, lastErr = net.DialTimeout("tcp", addr, dialInterval)
		if lastErr == nil {
			break
		}
		log.Printf("proxy: attempt %d: dial %s failed: %v", attempt, addr, lastErr)
		time.Sleep(dialInterval)
	}

	if upstream == nil {
		log.Printf("proxy: exhausted retries connecting to \033[33m%s\033[0m: %v", ts.info.ContainerName, lastErr)
		return
	}
	upstreamAddr = upstream.RemoteAddr().String()
	defer upstream.Close() //nolint:errcheck

	defer func() { ts.lastActive = time.Now() }()

	log.Printf("proxy: proxying connection to %s", upstream.RemoteAddr())

	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			conn.Close()      //nolint:errcheck
			upstream.Close() //nolint:errcheck
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(buf)
		io.CopyBuffer(countingWriter{upstream, &bytesSent}, conn, *buf) //nolint:errcheck
		closeAll()
	}()

	go func() {
		defer wg.Done()
		buf := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(buf)
		io.CopyBuffer(countingWriter{conn, &bytesReceived}, upstream, *buf) //nolint:errcheck
		closeAll()
	}()

	wg.Wait()
	log.Printf("proxy: connection to \033[33m%s\033[0m closed", ts.info.ContainerName)
}

// cascadeStart starts all registered dependants of upstream.
func (s *ProxyServer) cascadeStart(upstream types.TargetInfo) {
	for _, depName := range upstream.Dependants {
		s.mu.RLock()
		depID, ok := s.nameToID[depName]
		s.mu.RUnlock()

		if !ok {
			log.Printf("proxy: cascade start: \033[33m%s\033[0m → %q not registered, skipping",
				upstream.ContainerName, depName)
			continue
		}
		log.Printf("proxy: cascade start: \033[33m%s\033[0m → \033[33m%s\033[0m",
			upstream.ContainerName, depName)
		_, startErr, shared := s.startGroup.Do(depID, func() (any, error) {
			return nil, s.backend.EnsureRunning(s.ctx, depID)
		})
		if shared {
			log.Printf("proxy: cascade start: joined in-flight startup for \033[33m%s\033[0m", depName)
		}
		if startErr != nil {
			log.Printf("proxy: cascade start: error starting \033[33m%s\033[0m: %v", depName, startErr)
			continue
		}
		s.mu.RLock()
		for _, ts := range s.targets {
			if ts.info.ContainerID == depID {
				ts.running = true
			}
		}
		for _, uls := range s.udpTargets {
			if uls.info.ContainerID == depID {
				uls.running = true
			}
		}
		if ds, ok := s.dependantStates[depID]; ok {
			ds.running = true
		}
		s.mu.RUnlock()
	}
}

// cascadeStop stops all registered dependants of upstream that are still running.
func (s *ProxyServer) cascadeStop(upstream types.TargetInfo) {
	for _, depName := range upstream.Dependants {
		s.mu.RLock()
		depID, ok := s.nameToID[depName]
		// Check whether any mapping for this dependant is still running.
		running := false
		for _, ts := range s.targets {
			if ts.info.ContainerID == depID && ts.running {
				running = true
				break
			}
		}
		if !running {
			for _, uls := range s.udpTargets {
				if uls.info.ContainerID == depID && uls.running {
					running = true
					break
				}
			}
		}
		if !running {
			if ds, ok := s.dependantStates[depID]; ok && ds.running {
				running = true
			}
		}
		s.mu.RUnlock()

		if !ok {
			log.Printf("proxy: cascade stop: \033[33m%s\033[0m → %q not registered, skipping",
				upstream.ContainerName, depName)
			continue
		}
		if !running {
			continue // already stopped
		}
		log.Printf("proxy: cascade stop: \033[33m%s\033[0m → \033[33m%s\033[0m",
			upstream.ContainerName, depName)
		if err := s.backend.StopContainer(s.ctx, depID, depName); err != nil {
			log.Printf("proxy: cascade stop: error stopping \033[33m%s\033[0m: %v", depName, err)
			continue
		}
		s.mu.RLock()
		for _, ts := range s.targets {
			if ts.info.ContainerID == depID {
				ts.running = false
			}
		}
		for _, uls := range s.udpTargets {
			if uls.info.ContainerID == depID {
				uls.running = false
			}
		}
		if ds, ok := s.dependantStates[depID]; ok {
			ds.running = false
		}
		s.mu.RUnlock()
	}
}
