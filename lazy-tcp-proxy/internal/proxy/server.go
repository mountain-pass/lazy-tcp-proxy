package proxy

import (
	"bufio"
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/metrics"
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
	ContainerMissing   bool       `json:"container_missing"`
	ActiveConns        int32      `json:"active_conns"`
	LastActive         *time.Time `json:"last_active"`
	LastActiveRelative string     `json:"last_active_relative"`
	TraefikHosts       []string   `json:"traefik_hosts,omitempty"`
	TraefikTCPHosts    []string   `json:"traefik_tcp_hosts,omitempty"`
	IsUDP              bool       `json:"is_udp"`
	HasAuth            bool       `json:"has_auth"`
	Availability       string     `json:"availability"`
	HasComposeFile     bool       `json:"has_compose_file"`
	HasTarGz           bool       `json:"has_tar_gz"`
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

var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, copyBufSize)
		return &b
	},
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
	missing         bool
	removed         bool
	tlsEnabled      bool
	apiKey          []string
	basicAuth       []string
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

// cronOnlyState tracks a container registered only for cron scheduling (no ports).
type cronOnlyState struct {
	info    types.TargetInfo
	running bool
}

// ProxyServer manages TCP listeners and proxies connections to targets.
type ProxyServer struct {
	backend        containerBackend
	ctx            context.Context
	mu             sync.RWMutex
	targets        map[int]*targetState     // keyed by TCP listen port
	udpTargets     map[int]*udpListenerState // keyed by UDP listen port
	cronOnlyTargets map[string]*cronOnlyState // keyed by ContainerID; port-less cron containers
	nameToID       map[string]string        // ContainerName → ContainerID for cascade lookup
	pollInterval  time.Duration
	idleTimeout   time.Duration
	startTimeout  time.Duration
	startTime     time.Time
	webhookClient *http.Client
	sched         cronScheduler      // nil if no scheduler configured
	startGroup    singleflight.Group // deduplicates concurrent EnsureRunning calls per container
	tlsConfig     *tls.Config        // shared self-signed cert; nil if cert generation failed
	collector     *metrics.Collector // nil if metrics are disabled
	composeDir    string             // directory scanned for compose files and image archives
}

// SetComposeDir sets the compose directory used to populate HasComposeFile and HasTarGz
// in status snapshots. An empty string disables the file checks.
func (s *ProxyServer) SetComposeDir(dir string) { s.composeDir = dir }

// composeFlags checks whether a compose file and/or image archive exist for name.
func (s *ProxyServer) composeFlags(name string) (hasCompose, hasTar bool) {
	if s.composeDir == "" {
		return false, false
	}
	for _, ext := range []string{".yml", ".yaml"} {
		if _, err := os.Stat(filepath.Join(s.composeDir, name+ext)); err == nil {
			hasCompose = true
			break
		}
	}
	if _, err := os.Stat(filepath.Join(s.composeDir, name+".tar.gz")); err == nil {
		hasTar = true
	}
	return
}

// NewServer creates a new ProxyServer backed by the given backend.
func NewServer(ctx context.Context, b containerBackend, startTime time.Time, idleTimeout, pollInterval, startTimeout time.Duration, tlsConfig *tls.Config) *ProxyServer {
	return &ProxyServer{
		backend:         b,
		ctx:             ctx,
		targets:         make(map[int]*targetState),
		udpTargets:      make(map[int]*udpListenerState),
		cronOnlyTargets: make(map[string]*cronOnlyState),
		nameToID:        make(map[string]string),
		idleTimeout:   idleTimeout,
		startTimeout:  startTimeout,
		pollInterval:  pollInterval,
		startTime:     startTime,
		webhookClient: &http.Client{Timeout: 5 * time.Second},
		tlsConfig:     tlsConfig,
	}
}

// SetScheduler injects the cron scheduler. Must be called before Discover.
func (s *ProxyServer) SetScheduler(sched cronScheduler) {
	s.sched = sched
}

// SetCollector injects the metrics collector and registers all already-tracked ports.
// Ports discovered before this call are missed if the collector isn't set first, so
// this back-fills them to handle the case where metrics initialisation happens after
// initial target discovery.
func (s *ProxyServer) SetCollector(c *metrics.Collector) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	s.collector = c
	for _, ts := range s.targets {
		c.RegisterPort(ts.listenPort, ts.info.ContainerName, false, types.EffectiveAvailability(ts.info))
		if ts.running {
			c.OnContainerRunning(ts.listenPort, false, now)
		}
	}
	for _, uls := range s.udpTargets {
		c.RegisterPort(uls.listenPort, uls.info.ContainerName, true, types.EffectiveAvailability(uls.info))
		if uls.running {
			c.OnContainerRunning(uls.listenPort, true, now)
		}
	}
}

// countingWriter wraps an io.Writer and accumulates the number of bytes written.
// Not safe for concurrent use — each goroutine should use its own instance.
type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	written, err := cw.w.Write(p)
	cw.n += int64(written)
	return written, err
}

// CronStart is called by the scheduler to start a target on its cron-start schedule.
// It is a no-op (with a log) if the target is already running.
func (s *ProxyServer) CronStart(ctx context.Context, targetID, targetName string) {
	s.mu.RLock()
	running, found := s.isRunning(targetID)
	_, isCronOnly := s.cronOnlyTargets[targetID]
	s.mu.RUnlock()
	if !found {
		log.Printf("scheduler: cron-start: \033[33m%s\033[0m not found, skipping", targetName)
		return
	}
	// For cron-only containers (no ports) skip the in-memory running check —
	// there are no active connections to track state, so it can be stale.
	// docker start is idempotent if the container is already running.
	if running && !isCronOnly {
		log.Printf("scheduler: cron-start: \033[33m%s\033[0m already running, no action", targetName)
		return
	}
	if err := s.backend.EnsureRunning(ctx, targetID); err != nil {
		log.Printf("scheduler: cron-start: failed to start \033[33m%s\033[0m: %v", targetName, err)
		return
	}
	now := time.Now()
	s.mu.Lock()
	for _, ts := range s.targets {
		if ts.info.ContainerID == targetID {
			ts.running = true
			if s.collector != nil {
				s.collector.RecordContainerStart(ts.listenPort, false)
				s.collector.OnContainerRunning(ts.listenPort, false, now)
			}
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == targetID {
			uls.running = true
			if s.collector != nil {
				s.collector.RecordContainerStart(uls.listenPort, true)
				s.collector.OnContainerRunning(uls.listenPort, true, now)
			}
		}
	}
	if ct, ok := s.cronOnlyTargets[targetID]; ok {
		ct.running = true
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
	if info.ContainerID == "" {
		if ct, ok := s.cronOnlyTargets[targetID]; ok {
			info = ct.info
		}
	}
	s.mu.RUnlock()
	if info.WebhookURL != "" {
		go s.fireWebhook(info.WebhookURL, "container_started", targetID, targetName, "", "", 0)
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
			if s.collector != nil {
				s.collector.ContainerStopped(ts.listenPort, false)
			}
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == targetID {
			uls.running = false
			info = uls.info
			if s.collector != nil {
				s.collector.ContainerStopped(uls.listenPort, true)
			}
		}
	}
	if ct, ok := s.cronOnlyTargets[targetID]; ok {
		ct.running = false
		if info.ContainerID == "" {
			info = ct.info
		}
	}
	s.mu.Unlock()
	log.Printf("scheduler: cron-stop: stopped \033[33m%s\033[0m", targetName)
	if info.WebhookURL != "" {
		go s.fireWebhook(info.WebhookURL, "container_stopped", targetID, targetName, "", "", 0)
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
	if ct, ok := s.cronOnlyTargets[targetID]; ok {
		found = true
		if ct.running {
			running = true
		}
	}
	return
}

// Snapshot returns a point-in-time copy of all registered targets.
func (s *ProxyServer) Snapshot() []TargetSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]TargetSnapshot, 0, len(s.targets)+len(s.udpTargets)+len(s.cronOnlyTargets))
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
		hasCompose, hasTar := s.composeFlags(ts.info.ContainerName)
		out = append(out, TargetSnapshot{
			ContainerID:        id,
			ContainerName:      ts.info.ContainerName,
			ListenPort:         listenPort,
			TargetPort:         ts.targetPort,
			Running:            ts.running,
			ContainerMissing:   ts.missing,
			ActiveConns:        ts.activeConns.Load(),
			LastActive:         &t,
			LastActiveRelative: relativeTime(effective, now),
			TraefikHosts:       ts.info.TraefikHosts,
			TraefikTCPHosts:    ts.info.TraefikTCPHosts,
			IsUDP:              false,
			HasAuth:            len(ts.apiKey) > 0 || len(ts.basicAuth) > 0,
			Availability:       types.EffectiveAvailability(ts.info),
			HasComposeFile:     hasCompose,
			HasTarGz:           hasTar,
		})
	}
	for listenPort, uls := range s.udpTargets {
		uls.mu.Lock()
		lastAct := uls.lastActive
		uls.mu.Unlock()
		if lastAct.IsZero() {
			lastAct = s.startTime
		}
		t := lastAct
		id := uls.info.ContainerID
		if len(id) > 12 {
			id = id[:12]
		}
		hasCompose, hasTar := s.composeFlags(uls.info.ContainerName)
		out = append(out, TargetSnapshot{
			ContainerID:        id,
			ContainerName:      uls.info.ContainerName,
			ListenPort:         listenPort,
			TargetPort:         uls.targetPort,
			Running:            uls.running,
			ContainerMissing:   uls.missing,
			ActiveConns:        uls.activeFlows.Load(),
			LastActive:         &t,
			LastActiveRelative: relativeTime(lastAct, now),
			TraefikHosts:       uls.info.TraefikHosts,
			TraefikTCPHosts:    uls.info.TraefikTCPHosts,
			IsUDP:              true,
			HasAuth:            false,
			Availability:       types.EffectiveAvailability(uls.info),
			HasComposeFile:     hasCompose,
			HasTarGz:           hasTar,
		})
	}
	for _, ct := range s.cronOnlyTargets {
		id := ct.info.ContainerID
		if len(id) > 12 {
			id = id[:12]
		}
		hasCompose, hasTar := s.composeFlags(ct.info.ContainerName)
		t := s.startTime
		out = append(out, TargetSnapshot{
			ContainerID:        id,
			ContainerName:      ct.info.ContainerName,
			Running:            ct.running,
			ContainerMissing:   ct.info.Missing,
			LastActive:         &t,
			LastActiveRelative: relativeTime(t, now),
			Availability:       types.EffectiveAvailability(ct.info),
			HasComposeFile:     hasCompose,
			HasTarGz:           hasTar,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContainerName != out[j].ContainerName {
			return out[i].ContainerName < out[j].ContainerName
		}
		if out[i].IsUDP != out[j].IsUDP {
			return !out[i].IsUDP // TCP before UDP
		}
		return out[i].ListenPort < out[j].ListenPort
	})
	return out
}

// fireWebhook POSTs a lifecycle event to the container's webhook URL.
// connID, remoteAddr and remotePort are included for connection/flow events;
// pass "", "", 0 for container lifecycle events.
// Must be called in a goroutine — never blocks the proxy path.
func (s *ProxyServer) fireWebhook(webhookURL, event, containerID, containerName, connID, remoteAddr string, remotePort int) {
	id := containerID
	if len(id) > 12 {
		id = id[:12]
	}
	payload := webhookPayload{
		Event:         event,
		ConnectionID:  connID,
		RemoteAddr:    remoteAddr,
		RemotePort:    remotePort,
		ContainerID:   id,
		ContainerName: containerName,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	resp, err := s.webhookClient.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("proxy: webhook: POST %s event=%s error: %v", webhookURL, event, err)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("proxy: webhook: POST %s event=%s non-2xx response: %d", webhookURL, event, resp.StatusCode)
		return
	}
	log.Printf("proxy: webhook: delivered event=%s to %s (%d)", event, webhookURL, resp.StatusCode)
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
			existing.missing = info.Missing
			existing.removed = false
			existing.tlsEnabled = info.TLS
			existing.apiKey = info.APIKey
			existing.basicAuth = info.BasicAuth
			if s.collector != nil {
				s.collector.RegisterPort(m.ListenPort, info.ContainerName, false, types.EffectiveAvailability(info))
			}
			log.Printf("proxy: updated TCP target \033[33m%s\033[0m on port %d->%d", info.ContainerName, m.ListenPort, m.TargetPort)
			continue
		}

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", m.ListenPort))
		if err != nil {
			log.Printf("proxy: failed to listen on TCP port %d for \033[33m%s\033[0m: %v", m.ListenPort, info.ContainerName, err)
			continue
		}
		if info.TLS {
			if s.tlsConfig == nil {
				log.Printf("proxy: TLS requested for \033[33m%s\033[0m port %d but TLS config unavailable; falling back to plain TCP",
					info.ContainerName, m.ListenPort)
			} else {
				ln = tls.NewListener(ln, s.tlsConfig)
				log.Printf("proxy: TLS enabled for \033[33m%s\033[0m port %d", info.ContainerName, m.ListenPort)
			}
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
			missing:         info.Missing,
			tlsEnabled:      info.TLS,
			apiKey:          info.APIKey,
			basicAuth:       info.BasicAuth,
		}
		s.targets[m.ListenPort] = ts
		if s.collector != nil {
			s.collector.RegisterPort(m.ListenPort, info.ContainerName, false, types.EffectiveAvailability(info))
		}
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
			existing.missing = info.Missing
			existing.removed = false
			if s.collector != nil {
				s.collector.RegisterPort(m.ListenPort, info.ContainerName, true, types.EffectiveAvailability(info))
			}
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
			missing:      info.Missing,
			flows:        make(map[string]*udpFlow),
			pending:      make(map[string]bool),
		}
		uls.upstreamReadyCond = sync.NewCond(&uls.mu)
		s.udpTargets[m.ListenPort] = uls
		if s.collector != nil {
			s.collector.RegisterPort(m.ListenPort, info.ContainerName, true, types.EffectiveAvailability(info))
		}
		log.Printf("proxy: registered target \033[33m%s\033[0m, UDP %d->%d", info.ContainerName, m.ListenPort, m.TargetPort)
		go s.udpReadLoop(uls)
		go s.udpFlowSweeper(s.ctx, uls, s.pollInterval)
	}

	// Keep name→ID map current for cascade lookups.
	s.nameToID[info.ContainerName] = info.ContainerID

	// Register with cron scheduler only when the effective availability is cron
	// and at least one cron expression is present.
	if s.sched != nil &&
		types.EffectiveAvailability(info) == types.AvailabilityCron &&
		(info.CronStart != "" || info.CronStop != "") {
		s.sched.Register(info)
	}

	// Track port-less cron-only containers so CronStart/CronStop can find them.
	if len(info.Ports) == 0 && len(info.UDPPorts) == 0 {
		s.cronOnlyTargets[info.ContainerID] = &cronOnlyState{info: info, running: info.Running}
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
			if s.collector != nil {
				s.collector.UnregisterPort(port, false)
			}
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
			if s.collector != nil {
				s.collector.UnregisterPort(port, true)
			}
			if err := uls.listenConn.Close(); err != nil {
				log.Printf("proxy: error closing UDP listener on port %d: %v", port, err)
			}
			delete(s.udpTargets, port)
		}
	}
	if ct, ok := s.cronOnlyTargets[containerID]; ok {
		log.Printf("proxy: removing cron-only target \033[33m%s\033[0m", ct.info.ContainerName)
		delete(s.nameToID, ct.info.ContainerName)
		delete(s.cronOnlyTargets, containerID)
	}
	if s.sched != nil {
		s.sched.Unregister(containerID)
	}
}

// currentTargetsByID returns a snapshot of all currently registered targets
// keyed by ContainerID. Multiple port mappings sharing the same ContainerID
// produce a single entry (last writer wins, which is fine for equality checks).
func (s *ProxyServer) currentTargetsByID() map[string]types.TargetInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]types.TargetInfo, len(s.targets)+len(s.udpTargets)+len(s.cronOnlyTargets))
	for _, ts := range s.targets {
		out[ts.info.ContainerID] = ts.info
	}
	for _, uls := range s.udpTargets {
		out[uls.info.ContainerID] = uls.info
	}
	for id, ct := range s.cronOnlyTargets {
		out[id] = ct.info
	}
	return out
}

// targetInfoEqual reports whether two TargetInfos have identical proxy
// configuration. Backend-managed fields (ContainerID, NetworkIDs, Running,
// HasHealthCheck) are intentionally excluded.
func targetInfoEqual(a, b types.TargetInfo) bool {
	return reflect.DeepEqual(a.Ports, b.Ports) &&
		reflect.DeepEqual(a.UDPPorts, b.UDPPorts) &&
		reflect.DeepEqual(a.AllowList, b.AllowList) &&
		reflect.DeepEqual(a.BlockList, b.BlockList) &&
		reflect.DeepEqual(a.IdleTimeout, b.IdleTimeout) &&
		reflect.DeepEqual(a.StartTimeout, b.StartTimeout) &&
		a.WebhookURL == b.WebhookURL &&
		reflect.DeepEqual(a.Dependants, b.Dependants) &&
		a.CronStart == b.CronStart &&
		a.CronStop == b.CronStop &&
		a.Availability == b.Availability &&
		a.HTTPHealthCheck == b.HTTPHealthCheck &&
		a.TLS == b.TLS &&
		reflect.DeepEqual(a.APIKey, b.APIKey) &&
		reflect.DeepEqual(a.BasicAuth, b.BasicAuth) &&
		reflect.DeepEqual(a.TraefikHosts, b.TraefikHosts) &&
		reflect.DeepEqual(a.TraefikTCPHosts, b.TraefikTCPHosts)
}

// Update reconciles the proxy's registered targets with newTargets.
// Targets absent from newTargets are removed; new targets are registered;
// changed targets are re-registered (brief port-unbound window is acceptable
// on a config reload path).
func (s *ProxyServer) Update(newTargets []types.TargetInfo) {
	current := s.currentTargetsByID()

	newByID := make(map[string]types.TargetInfo, len(newTargets))
	for _, t := range newTargets {
		newByID[t.ContainerID] = t
	}

	for id := range current {
		if _, ok := newByID[id]; !ok {
			s.RemoveTarget(id)
		}
	}

	for id, newInfo := range newByID {
		if cur, exists := current[id]; !exists {
			s.RegisterTarget(newInfo)
		} else if !targetInfoEqual(cur, newInfo) {
			s.RemoveTarget(id)
			s.RegisterTarget(newInfo)
		}
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
			if s.collector != nil {
				s.collector.ContainerStopped(ts.listenPort, false)
			}
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == containerID {
			uls.running = false
			info = uls.info
			affectedULS = append(affectedULS, uls)
			if s.collector != nil {
				s.collector.ContainerStopped(uls.listenPort, true)
			}
		}
	}
	if ct, ok := s.cronOnlyTargets[containerID]; ok {
		ct.running = false
		if info.ContainerID == "" {
			info = ct.info
		}
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

// ContainerRemoved marks all port mappings for a config-only container as
// missing (i.e. destroyed by pruning). The listeners are kept so the container
// recovers automatically when it is recreated.
func (s *ProxyServer) ContainerRemoved(containerID string) {
	s.mu.RLock()
	var affectedULS []*udpListenerState
	for _, ts := range s.targets {
		if ts.info.ContainerID == containerID {
			ts.running = false
			ts.missing = true
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == containerID {
			uls.running = false
			uls.missing = true
			affectedULS = append(affectedULS, uls)
		}
	}
	s.mu.RUnlock()
	for _, uls := range affectedULS {
		uls.mu.Lock()
		uls.upstreamReady = false
		if uls.upstreamStarting {
			uls.upstreamStarting = false
			uls.upstreamReadyCond.Broadcast()
		}
		uls.mu.Unlock()
	}
}

// ContainerStarted marks all port mappings for the container as running and
// cascades a start to any declared dependants.
func (s *ProxyServer) ContainerStarted(containerID string) {
	s.mu.Lock()
	now := time.Now()
	var info types.TargetInfo
	for _, ts := range s.targets {
		if ts.info.ContainerID == containerID {
			ts.running = true
			ts.missing = false
			info = ts.info
			if s.collector != nil {
				s.collector.OnContainerRunning(ts.listenPort, false, now)
			}
		}
	}
	for _, uls := range s.udpTargets {
		if uls.info.ContainerID == containerID {
			uls.running = true
			uls.missing = false
			if s.collector != nil {
				s.collector.OnContainerRunning(uls.listenPort, true, now)
			}
		}
	}
	if ct, ok := s.cronOnlyTargets[containerID]; ok {
		ct.running = true
		ct.info.Missing = false
		if info.ContainerID == "" {
			info = ct.info
		}
	}
	s.mu.Unlock()
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
		if types.EffectiveAvailability(ts.info) != types.AvailabilityOnDemand {
			continue // lifecycle not managed on-demand
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
		if types.EffectiveAvailability(uls.info) != types.AvailabilityOnDemand {
			continue // lifecycle not managed on-demand
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
					if s.collector != nil {
						s.collector.ContainerStopped(ts.listenPort, false)
					}
				}
				for _, uls := range e.udpStates {
					uls.running = false
					if s.collector != nil {
						s.collector.ContainerStopped(uls.listenPort, true)
					}
				}
				if e.webhookURL != "" {
					go s.fireWebhook(e.webhookURL, "container_stopped", e.containerID, e.name, "", "", 0)
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

	connStart := time.Now()
	if s.collector != nil {
		s.collector.ConnectionStarted(ts.listenPort, false)
		defer func() {
			s.collector.ConnectionEnded(ts.listenPort, false, time.Since(connStart).Milliseconds())
		}()
	}

	log.Printf("proxy: new connection to \033[33m%s\033[0m (port %d) from \033[36m%s\033[0m",
		ts.info.ContainerName, ts.targetPort, conn.RemoteAddr())

	remoteIP, remotePortStr, _ := net.SplitHostPort(conn.RemoteAddr().String())
	remotePort, _ := strconv.Atoi(remotePortStr)
	connID := newConnectionID()
	if ts.info.WebhookURL != "" {
		go s.fireWebhook(ts.info.WebhookURL, "tcp_conn_start", ts.info.ContainerID, ts.info.ContainerName, connID, remoteIP, remotePort)
		defer func() {
			go s.fireWebhook(ts.info.WebhookURL, "tcp_conn_end", ts.info.ContainerID, ts.info.ContainerName, connID, remoteIP, remotePort)
		}()
	}

	if ts.info.Availability != types.AvailabilityCron && ts.info.Availability != types.AvailabilityManual {
		coldStart := time.Now()
		_, startErr, shared := s.startGroup.Do(ts.info.ContainerID, func() (any, error) {
			return nil, s.backend.EnsureRunning(ctx, ts.info.ContainerID)
		})
		if shared {
			log.Printf("proxy: joined in-flight startup for \033[33m%s\033[0m", ts.info.ContainerName)
		}
		if startErr != nil {
			log.Printf("proxy: could not start container \033[33m%s\033[0m: %v", ts.info.ContainerName, startErr)
			if s.collector != nil {
				s.collector.ConnectionFailed(ts.listenPort, false)
			}
			return
		}
		now := time.Now()
		if !shared && s.collector != nil {
			s.collector.RecordColdStart(ts.listenPort, false, time.Since(coldStart).Milliseconds())
		}
		s.mu.Lock()
		for _, t := range s.targets {
			if t.info.ContainerID == ts.info.ContainerID {
				t.running = true
				if s.collector != nil {
					s.collector.OnContainerRunning(t.listenPort, false, now)
				}
			}
		}
		for _, u := range s.udpTargets {
			if u.info.ContainerID == ts.info.ContainerID {
				u.running = true
				if s.collector != nil {
					s.collector.OnContainerRunning(u.listenPort, true, now)
				}
			}
		}
		s.mu.Unlock()
		if ts.info.WebhookURL != "" {
			go s.fireWebhook(ts.info.WebhookURL, "container_started", ts.info.ContainerID, ts.info.ContainerName, "", "", 0)
		}
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
		if s.collector != nil {
			s.collector.ConnectionFailed(ts.listenPort, false)
		}
		return
	}
	defer upstream.Close() //nolint:errcheck

	log.Printf("proxy: proxying connection to %s", upstream.RemoteAddr())

	if len(ts.apiKey) > 0 || len(ts.basicAuth) > 0 {
		sent, recv := s.handleHTTPProxy(conn, upstream, ts)
		if s.collector != nil {
			s.collector.AddBytes(ts.listenPort, false, sent, recv)
		}
		ts.lastActive = time.Now()
		log.Printf("proxy: connection to \033[33m%s\033[0m closed", ts.info.ContainerName)
		return
	}

	defer func() { ts.lastActive = time.Now() }()

	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			conn.Close()      //nolint:errcheck
			upstream.Close() //nolint:errcheck
		})
	}

	var sentCW, recvCW countingWriter
	sentCW.w = upstream
	recvCW.w = conn

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(buf)
		io.CopyBuffer(&sentCW, conn, *buf) //nolint:errcheck
		closeAll()
	}()

	go func() {
		defer wg.Done()
		buf := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(buf)
		io.CopyBuffer(&recvCW, upstream, *buf) //nolint:errcheck
		closeAll()
	}()

	wg.Wait()
	if s.collector != nil {
		s.collector.AddBytes(ts.listenPort, false, sentCW.n, recvCW.n)
	}
	log.Printf("proxy: connection to \033[33m%s\033[0m closed", ts.info.ContainerName)
}

// handleHTTPProxy handles a connection in HTTP mode: reads each request,
// enforces X-API-Key, strips the header, and forwards to upstream.
// Supports HTTP/1.1 keep-alive. Returns total bytes sent to upstream and received from upstream.
func (s *ProxyServer) handleHTTPProxy(client, upstream net.Conn, ts *targetState) (bytesSent, bytesRecv int64) {
	var sentCW, recvCW countingWriter
	sentCW.w = upstream
	recvCW.w = client
	br := bufio.NewReader(client)
	ubr := bufio.NewReader(upstream)

	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return // EOF or malformed request — connection done
		}

		if len(ts.basicAuth) > 0 {
			authHeader := req.Header.Get("Authorization")
			const prefix = "Basic "
			ok := false
			if strings.HasPrefix(authHeader, prefix) {
				decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
				if err == nil {
					colonIdx := strings.IndexByte(string(decoded), ':')
					if colonIdx >= 0 {
						inUser := decoded[:colonIdx]
						inPass := decoded[colonIdx+1:]
						for _, cred := range ts.basicAuth {
							sep := strings.IndexByte(cred, ':')
							if sep < 0 {
								continue
							}
							storedUser := []byte(cred[:sep])
							storedHash := []byte(cred[sep+1:])
							if subtle.ConstantTimeCompare(inUser, storedUser) == 1 &&
								bcrypt.CompareHashAndPassword(storedHash, inPass) == nil {
								ok = true
								break
							}
						}
					}
				}
			}
			if !ok {
				log.Printf("proxy: basic-auth: rejected request to \033[33m%s\033[0m from \033[36m%s\033[0m (bad or missing credentials)",
					ts.info.ContainerName, client.RemoteAddr())
				client.Write([]byte( //nolint:errcheck
					"HTTP/1.1 401 Unauthorized\r\n" +
						"WWW-Authenticate: Basic realm=\"lazy-tcp-proxy\"\r\n" +
						"Content-Length: 0\r\n" +
						"Connection: close\r\n\r\n"))
				return
			}
			req.Header.Del("Authorization")
		}

		if len(ts.apiKey) > 0 {
			got := req.Header.Get("X-API-Key")
			ok := false
			for _, k := range ts.apiKey {
				if subtle.ConstantTimeCompare([]byte(got), []byte(k)) == 1 {
					ok = true
					break
				}
			}
			if !ok {
				log.Printf("proxy: api-key: rejected request to \033[33m%s\033[0m from \033[36m%s\033[0m (bad or missing key)",
					ts.info.ContainerName, client.RemoteAddr())
				client.Write([]byte( //nolint:errcheck
					"HTTP/1.1 401 Unauthorized\r\n" +
						"Content-Length: 0\r\n" +
						"Connection: close\r\n\r\n"))
				return
			}
			req.Header.Del("X-API-Key")
		}

		if err := req.Write(&sentCW); err != nil {
			return sentCW.n, recvCW.n
		}
		if req.Body != nil {
			req.Body.Close() //nolint:errcheck
		}

		resp, err := http.ReadResponse(ubr, req)
		if err != nil {
			return sentCW.n, recvCW.n
		}
		keepAlive := req.ProtoAtLeast(1, 1) && !resp.Close
		if err := resp.Write(&recvCW); err != nil {
			resp.Body.Close() //nolint:errcheck
			return sentCW.n, recvCW.n
		}
		resp.Body.Close() //nolint:errcheck

		ts.lastActive = time.Now()

		if !keepAlive {
			return sentCW.n, recvCW.n
		}
	}
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
		s.mu.RUnlock()
	}
}
