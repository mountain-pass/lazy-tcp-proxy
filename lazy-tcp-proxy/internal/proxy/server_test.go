package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

// ---- effectiveTimeout ----

func TestEffectiveTimeout_NoOverride(t *testing.T) {
	global := 2 * time.Minute
	if got := effectiveTimeout(nil, global); got != global {
		t.Errorf("got %s, want %s", got, global)
	}
}

func TestEffectiveTimeout_WithOverride(t *testing.T) {
	global := 2 * time.Minute
	override := 30 * time.Second
	if got := effectiveTimeout(&override, global); got != override {
		t.Errorf("got %s, want %s", got, override)
	}
}

func TestEffectiveTimeout_ZeroOverride(t *testing.T) {
	global := 2 * time.Minute
	zero := time.Duration(0)
	if got := effectiveTimeout(&zero, global); got != 0 {
		t.Errorf("got %s, want 0s", got)
	}
}

// ---- ipBlocked ----

func TestIPBlocked_NoLists(t *testing.T) {
	info := types.TargetInfo{} // empty allow/block lists
	if ipBlocked("10.0.0.1:1234", info) {
		t.Error("expected not blocked with no lists")
	}
}

func TestIPBlocked_AllowList_MatchAllows(t *testing.T) {
	info := types.TargetInfo{
		AllowList: mustParseNets("192.168.0.0/16"),
	}
	if ipBlocked("192.168.1.100:1234", info) {
		t.Error("192.168.1.100 should be allowed by 192.168.0.0/16")
	}
}

func TestIPBlocked_AllowList_NoMatchBlocks(t *testing.T) {
	info := types.TargetInfo{
		AllowList: mustParseNets("192.168.0.0/16"),
	}
	if !ipBlocked("10.0.0.1:1234", info) {
		t.Error("10.0.0.1 should be blocked — not in allow-list")
	}
}

func TestIPBlocked_BlockList_MatchBlocks(t *testing.T) {
	info := types.TargetInfo{
		BlockList: mustParseNets("10.0.0.1"),
	}
	if !ipBlocked("10.0.0.1:1234", info) {
		t.Error("10.0.0.1 should be blocked by block-list")
	}
}

func TestIPBlocked_BlockList_NoMatchAllows(t *testing.T) {
	info := types.TargetInfo{
		BlockList: mustParseNets("10.0.0.1"),
	}
	if ipBlocked("10.0.0.2:1234", info) {
		t.Error("10.0.0.2 should not be blocked")
	}
}

func TestIPBlocked_BlockListEvaluatedAfterAllowList(t *testing.T) {
	info := types.TargetInfo{
		AllowList: mustParseNets("10.0.0.0/8"),
		BlockList: mustParseNets("10.0.0.1"),
	}
	if !ipBlocked("10.0.0.1:1234", info) {
		t.Error("10.0.0.1 passes allow-list but is in block-list — should be blocked")
	}
}

func TestIPBlocked_NotInAllowList_BlockListNotEvaluated(t *testing.T) {
	info := types.TargetInfo{
		AllowList: mustParseNets("192.168.0.0/16"),
		BlockList: mustParseNets("10.0.0.1"),
	}
	if !ipBlocked("10.0.0.1:1234", info) {
		t.Error("10.0.0.1 not in allow-list, should be blocked")
	}
}

func TestIPBlocked_UnparsableAddr(t *testing.T) {
	info := types.TargetInfo{BlockList: mustParseNets("10.0.0.1")}
	if ipBlocked("not-an-addr", info) {
		t.Error("unparsable address should not be blocked")
	}
}

func TestIPBlocked_IPv6(t *testing.T) {
	info := types.TargetInfo{
		BlockList: mustParseNets("::1"),
	}
	if !ipBlocked("[::1]:1234", info) {
		t.Error("::1 should be blocked")
	}
}

// ---- Snapshot ----

func TestSnapshot_Empty(t *testing.T) {
	s := newTestServer()
	snap := s.Snapshot()
	if len(snap) != 0 {
		t.Errorf("expected empty snapshot, got %d entries", len(snap))
	}
}

func TestSnapshot_Fields(t *testing.T) {
	s := newTestServer()
	now := time.Now()
	ts := &targetState{
		info: types.TargetInfo{
			ContainerID:   strings.Repeat("a", 64),
			ContainerName: "my-service",
		},
		targetPort: 80,
		running:    true,
		lastActive: now,
	}
	ts.activeConns.Store(3)
	s.targets[9000] = ts

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 snapshot entry, got %d", len(snap))
	}
	e := snap[0]
	if e.ContainerID != strings.Repeat("a", 12) {
		t.Errorf("ContainerID: got %q, want 12-char prefix", e.ContainerID)
	}
	if e.ContainerName != "my-service" {
		t.Errorf("ContainerName: got %q", e.ContainerName)
	}
	if e.ListenPort != 9000 {
		t.Errorf("ListenPort: got %d, want 9000", e.ListenPort)
	}
	if e.TargetPort != 80 {
		t.Errorf("TargetPort: got %d, want 80", e.TargetPort)
	}
	if !e.Running {
		t.Error("Running: expected true")
	}
	if e.ActiveConns != 3 {
		t.Errorf("ActiveConns: got %d, want 3", e.ActiveConns)
	}
	if e.LastActive == nil {
		t.Error("LastActive: expected non-nil")
	}
	if e.LastActiveRelative == "" {
		t.Error("LastActiveRelative: expected non-empty string")
	}
}

func TestSnapshot_NeverActiveFallsBackToStartTime(t *testing.T) {
	s := newTestServer()
	s.targets[9000] = &targetState{
		info:       types.TargetInfo{ContainerID: "abc123"},
		targetPort: 80,
		lastActive: time.Time{}, // zero — never active
	}
	snap := s.Snapshot()
	if snap[0].LastActive == nil {
		t.Error("LastActive should not be nil; expected fallback to startTime")
	}
	if !snap[0].LastActive.Equal(s.startTime) {
		t.Errorf("LastActive should equal startTime %v, got %v", s.startTime, snap[0].LastActive)
	}
	if snap[0].LastActiveRelative == "" {
		t.Error("LastActiveRelative should be non-empty")
	}
}

// ---- RegisterTarget port conflict ----

func TestRegisterTarget_TCPPortConflict(t *testing.T) {
	s := newTestServer()

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("could not open test listener: %v", err)
	}
	defer ln.Close() //nolint:errcheck
	listenPort := ln.Addr().(*net.TCPAddr).Port

	s.targets[listenPort] = &targetState{
		info:     types.TargetInfo{ContainerID: "container-a", ContainerName: "svc-a"},
		listener: ln,
	}

	s.RegisterTarget(types.TargetInfo{
		ContainerID:   "container-b",
		ContainerName: "svc-b",
		Ports:         []types.PortMapping{{ListenPort: listenPort, TargetPort: 80}},
	})

	if s.targets[listenPort].info.ContainerID != "container-a" {
		t.Error("conflict: container-a's registration should not have been replaced")
	}
}

func TestRegisterTarget_UDPPortConflict(t *testing.T) {
	s := newTestServer()

	pc, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatalf("could not open test UDP listener: %v", err)
	}
	defer pc.Close() //nolint:errcheck
	listenPort := pc.LocalAddr().(*net.UDPAddr).Port

	s.udpTargets[listenPort] = &udpListenerState{
		listenConn: pc.(*net.UDPConn),
		info:       types.TargetInfo{ContainerID: "container-a", ContainerName: "svc-a"},
	}

	s.RegisterTarget(types.TargetInfo{
		ContainerID:   "container-b",
		ContainerName: "svc-b",
		Ports:         []types.PortMapping{{ListenPort: 19999, TargetPort: 80}},
		UDPPorts:      []types.PortMapping{{ListenPort: listenPort, TargetPort: 53}},
	})

	if s.udpTargets[listenPort].info.ContainerID != "container-a" {
		t.Error("conflict: container-a's UDP registration should not have been replaced")
	}
}

// ---- checkInactivity grouping ----

func TestCheckInactivity_StopsIdleContainer(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	s.targets[9000] = &targetState{
		info:       types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort: 80,
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
	}

	s.checkInactivity(context.Background())

	select {
	case id := <-stopped:
		if id != "ctr-1" {
			t.Errorf("expected ctr-1 to be stopped, got %s", id)
		}
	default:
		t.Error("expected StopContainer to be called")
	}
}

func TestCheckInactivity_DoesNotStopActiveConnections(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	ts := &targetState{
		info:       types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort: 80,
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
	}
	ts.activeConns.Store(1)
	s.targets[9000] = ts

	s.checkInactivity(context.Background())

	select {
	case <-stopped:
		t.Error("should not stop container with active connections")
	default:
	}
}

func TestCheckInactivity_DoesNotStopRecentlyActive(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	s.targets[9000] = &targetState{
		info:       types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort: 80,
		running:    true,
		lastActive: time.Now(),
	}

	s.checkInactivity(context.Background())

	select {
	case <-stopped:
		t.Error("should not stop recently active container")
	default:
	}
}

func TestCheckInactivity_DoesNotStopAlreadyStopped(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	s.targets[9000] = &targetState{
		info:       types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort: 80,
		running:    false,
		lastActive: time.Now().Add(-10 * time.Minute),
	}

	s.checkInactivity(context.Background())

	select {
	case <-stopped:
		t.Error("should not stop already-stopped container")
	default:
	}
}

func TestCheckInactivity_MultiPortAllIdleStops(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	info := types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"}
	for _, port := range []int{9000, 9001} {
		s.targets[port] = &targetState{
			info:       info,
			targetPort: 80,
			running:    true,
			lastActive: time.Now().Add(-10 * time.Minute),
		}
	}

	s.checkInactivity(context.Background())

	select {
	case id := <-stopped:
		if id != "ctr-1" {
			t.Errorf("expected ctr-1, got %s", id)
		}
	default:
		t.Error("expected stop when all ports are idle")
	}
}

func TestCheckInactivity_MultiPortOneActiveDoesNotStop(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	info := types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"}
	s.targets[9000] = &targetState{
		info:       info,
		targetPort: 80,
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
	}
	ts := &targetState{
		info:       info,
		targetPort: 8080,
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
	}
	ts.activeConns.Store(1)
	s.targets[9001] = ts

	s.checkInactivity(context.Background())

	select {
	case <-stopped:
		t.Error("should not stop when one port still has active connections")
	default:
	}
}

func TestCheckInactivity_UDPIdleWithNoTCPStops(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	pc, _ := net.ListenPacket("udp", ":0")
	defer pc.Close() //nolint:errcheck
	s.udpTargets[5353] = &udpListenerState{
		listenConn: pc.(*net.UDPConn),
		info:       types.TargetInfo{ContainerID: "ctr-udp", ContainerName: "dns"},
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
		flows:      make(map[string]*udpFlow),
		pending:    make(map[string]bool),
	}

	s.checkInactivity(context.Background())

	select {
	case id := <-stopped:
		if id != "ctr-udp" {
			t.Errorf("expected ctr-udp, got %s", id)
		}
	default:
		t.Error("expected UDP-only container to be stopped when idle")
	}
}

func TestCheckInactivity_PerContainerTimeout_ShortOverrideStops(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	override := 10 * time.Second
	s.targets[9000] = &targetState{
		info:        types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort:  80,
		running:     true,
		lastActive:  time.Now().Add(-1 * time.Minute),
		idleTimeout: &override,
	}

	s.checkInactivity(context.Background())

	select {
	case id := <-stopped:
		if id != "ctr-1" {
			t.Errorf("expected ctr-1, got %s", id)
		}
	default:
		t.Error("expected container to be stopped: per-container timeout exceeded")
	}
}

func TestCheckInactivity_PerContainerTimeout_LongOverrideDoesNotStop(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	override := 10 * time.Minute
	s.targets[9000] = &targetState{
		info:        types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort:  80,
		running:     true,
		lastActive:  time.Now().Add(-6 * time.Minute),
		idleTimeout: &override,
	}

	s.checkInactivity(context.Background())

	select {
	case <-stopped:
		t.Error("should not stop: lastActive is within the per-container 10-minute timeout")
	default:
	}
}

func TestCheckInactivity_ZeroPerContainerTimeout_StopsImmediately(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	zero := time.Duration(0)
	s.targets[9000] = &targetState{
		info:        types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"},
		targetPort:  80,
		running:     true,
		lastActive:  time.Now(),
		idleTimeout: &zero,
	}

	s.checkInactivity(context.Background())

	select {
	case id := <-stopped:
		if id != "ctr-1" {
			t.Errorf("expected ctr-1, got %s", id)
		}
	default:
		t.Error("expected container to be stopped immediately (timeout=0)")
	}
}

func TestCheckInactivity_TCPIdleButUDPActiveDoesNotStop(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })

	info := types.TargetInfo{ContainerID: "ctr-1", ContainerName: "svc"}

	s.targets[9000] = &targetState{
		info:       info,
		targetPort: 80,
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
	}

	pc, _ := net.ListenPacket("udp", ":0")
	defer pc.Close() //nolint:errcheck
	uls := &udpListenerState{
		listenConn: pc.(*net.UDPConn),
		info:       info,
		running:    true,
		lastActive: time.Now().Add(-10 * time.Minute),
		flows: map[string]*udpFlow{
			"10.0.0.1:5000": {lastActive: time.Now()},
		},
		pending: make(map[string]bool),
	}
	s.udpTargets[5353] = uls

	s.checkInactivity(context.Background())

	select {
	case <-stopped:
		t.Error("should not stop when UDP has active flows")
	default:
	}
}

// ---- helpers ----

// mockBackend satisfies containerBackend for unit tests.
type mockBackend struct {
	stopFunc     func(containerID string)
	startFunc    func(containerID string)
	upstreamHost string // returned by GetUpstreamHost; "" simulates unresolvable
	upstreamErr  error  // returned by GetUpstreamHost when non-nil
}

func (m *mockBackend) EnsureRunning(_ context.Context, id string) error {
	if m.startFunc != nil {
		m.startFunc(id)
	}
	return nil
}
func (m *mockBackend) GetUpstreamHost(_ context.Context, _, _ string) (string, error) {
	return m.upstreamHost, m.upstreamErr
}
func (m *mockBackend) StopContainer(_ context.Context, containerID, _ string) error {
	if m.stopFunc != nil {
		m.stopFunc(containerID)
	}
	return nil
}
func (m *mockBackend) WaitUntilHealthy(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}

func newTestServer() *ProxyServer {
	return &ProxyServer{
		ctx:          context.Background(),
		targets:      make(map[int]*targetState),
		udpTargets:   make(map[int]*udpListenerState),
		nameToID:     make(map[string]string),
		idleTimeout:  5 * time.Minute,
		pollInterval: 15 * time.Second,
		startTime:    time.Now().Add(-1 * time.Hour),
	}
}

func newTestServerWithStopper(stopFn func(id string)) *ProxyServer {
	s := newTestServer()
	s.backend = &mockBackend{stopFunc: stopFn}
	return s
}

func newTestServerWithStartStopper(startFn, stopFn func(id string)) *ProxyServer {
	s := newTestServer()
	s.backend = &mockBackend{startFunc: startFn, stopFunc: stopFn}
	return s
}

func newTestServerWithHTTPClient(client *http.Client) *ProxyServer {
	s := newTestServer()
	s.webhookClient = client
	return s
}

// ---- cascade ----

// populateCascadeTargets sets up hub→chrome in s.targets and s.nameToID directly,
// avoiding real port binding conflicts. hubRunning and chromeRunning control initial state.
func populateCascadeTargets(s *ProxyServer, hubRunning, chromeRunning bool) {
	hubInfo := types.TargetInfo{
		ContainerID:   "hub-id",
		ContainerName: "hub",
		Running:       hubRunning,
		Dependants:    []string{"chrome"},
	}
	chromeInfo := types.TargetInfo{
		ContainerID:   "chrome-id",
		ContainerName: "chrome",
		Running:       chromeRunning,
	}
	s.targets[9000] = &targetState{
		info:       hubInfo,
		targetPort: 4444,
		running:    hubRunning,
		lastActive: time.Now().Add(-10 * time.Minute), // hub is idle
	}
	s.targets[9001] = &targetState{
		info:       chromeInfo,
		targetPort: 5900,
		running:    chromeRunning,
		lastActive: time.Now(), // chrome is recently active — only stopped via cascade
	}
	s.nameToID["hub"] = "hub-id"
	s.nameToID["chrome"] = "chrome-id"
}

func TestCascadeStart_StartsRegisteredDependant(t *testing.T) {
	started := make(chan string, 1)
	s := newTestServerWithStartStopper(func(id string) { started <- id }, nil)
	populateCascadeTargets(s, true, false)

	s.ContainerStarted("hub-id")

	select {
	case id := <-started:
		if id != "chrome-id" {
			t.Errorf("expected chrome-id to be started, got %s", id)
		}
	case <-time.After(time.Second):
		t.Error("EnsureRunning was not called for chrome")
	}
}

func TestCascadeStart_SkipsUnknownDependant(t *testing.T) {
	started := make(chan string, 1)
	s := newTestServerWithStartStopper(func(id string) { started <- id }, nil)

	s.targets[9000] = &targetState{
		info: types.TargetInfo{
			ContainerID:   "hub-id",
			ContainerName: "hub",
			Dependants:    []string{"unknown-service"},
		},
		running: true,
	}
	s.nameToID["hub"] = "hub-id"

	s.ContainerStarted("hub-id")

	time.Sleep(50 * time.Millisecond)
	select {
	case id := <-started:
		t.Errorf("EnsureRunning should not have been called; got %s", id)
	default:
	}
}

func TestCascadeStart_NoDependants_NoAction(t *testing.T) {
	started := make(chan string, 1)
	s := newTestServerWithStartStopper(func(id string) { started <- id }, nil)

	s.targets[9000] = &targetState{
		info:    types.TargetInfo{ContainerID: "hub-id", ContainerName: "hub"},
		running: true,
	}
	s.nameToID["hub"] = "hub-id"

	s.ContainerStarted("hub-id")

	time.Sleep(50 * time.Millisecond)
	select {
	case id := <-started:
		t.Errorf("EnsureRunning should not have been called; got %s", id)
	default:
	}
}

func TestCascadeStart_UnknownContainerID_NoPanic(t *testing.T) {
	s := newTestServerWithStartStopper(nil, nil)
	// Should not panic for an unknown containerID.
	s.ContainerStarted("does-not-exist")
}

func TestCascadeStop_StopsRunningDependant(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })
	populateCascadeTargets(s, true, true)

	s.ContainerStopped("hub-id")

	select {
	case id := <-stopped:
		if id != "chrome-id" {
			t.Errorf("expected chrome-id to be stopped, got %s", id)
		}
	case <-time.After(time.Second):
		t.Error("StopContainer was not called for chrome")
	}
}

func TestCascadeStop_SkipsAlreadyStopped(t *testing.T) {
	stopped := make(chan string, 1)
	s := newTestServerWithStopper(func(id string) { stopped <- id })
	populateCascadeTargets(s, true, false) // chrome already stopped

	s.ContainerStopped("hub-id")

	time.Sleep(50 * time.Millisecond)
	select {
	case id := <-stopped:
		t.Errorf("StopContainer should not have been called for already-stopped chrome; got %s", id)
	default:
	}
}

func TestCascadeStop_TriggeredByCheckInactivity(t *testing.T) {
	stopped := make(chan string, 2)
	s := newTestServerWithStopper(func(id string) { stopped <- id })
	populateCascadeTargets(s, true, true)

	s.checkInactivity(context.Background())

	// hub should be stopped first (synchronous in checkInactivity).
	select {
	case id := <-stopped:
		if id != "hub-id" {
			t.Errorf("expected hub-id to be stopped first, got %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("hub was not stopped")
	}
	// chrome cascade stop fires in a goroutine.
	select {
	case id := <-stopped:
		if id != "chrome-id" {
			t.Errorf("expected chrome-id to be cascade-stopped, got %s", id)
		}
	case <-time.After(time.Second):
		t.Error("chrome was not cascade-stopped")
	}
}

// ---- relativeTime ----

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		ago      time.Duration
		expected string
	}{
		{"seconds", 10 * time.Second, "10 seconds ago"},
		{"boundary_minute", 60 * time.Second, "1 minutes ago"},
		{"minutes", 4 * time.Minute, "4 minutes ago"},
		{"boundary_hour", 60 * time.Minute, "1 hours ago"},
		{"hours", 8 * time.Hour, "8 hours ago"},
		{"boundary_day", 24 * time.Hour, "1 days ago"},
		{"days", 3 * 24 * time.Hour, "3 days ago"},
		{"boundary_month", 30 * 24 * time.Hour, "1 months ago"},
		{"months", 45 * 24 * time.Hour, "1 months ago"},
		{"boundary_year", 365 * 24 * time.Hour, "1 years ago"},
		{"years", 400 * 24 * time.Hour, "1 years ago"},
		{"multi_year", 800 * 24 * time.Hour, "2 years ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := relativeTime(now.Add(-tc.ago), now)
			if got != tc.expected {
				t.Errorf("relativeTime(%v ago): got %q, want %q", tc.ago, got, tc.expected)
			}
		})
	}
}

// mustParseNets is a test helper that parses CIDRs/IPs into []net.IPNet.
func mustParseNets(entries ...string) []net.IPNet {
	var out []net.IPNet
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry)
		if err == nil {
			out = append(out, *ipNet)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		out = append(out, net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out
}

// ---- waitForHTTPReady ----

func TestWaitForHTTPReady_Immediate200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestServerWithHTTPClient(srv.Client())
	err := s.waitForHTTPReady(context.Background(), srv.URL+"/health", "svc", 5*time.Second)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestWaitForHTTPReady_503ThenOK(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	s := newTestServerWithHTTPClient(srv.Client())
	err := s.waitForHTTPReady(context.Background(), srv.URL+"/health", "svc", 10*time.Second)
	if err != nil {
		t.Errorf("expected nil error after 503→503→200, got %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestWaitForHTTPReady_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := newTestServerWithHTTPClient(srv.Client())
	// 2-second budget with dialInterval=1s → at most 2 attempts before timeout error
	err := s.waitForHTTPReady(context.Background(), srv.URL+"/health", "svc", 2*time.Second)
	if err == nil {
		t.Error("expected error when server never returns 2xx")
	}
}

func TestWaitForHTTPReady_CtxCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	s := newTestServerWithHTTPClient(srv.Client())
	err := s.waitForHTTPReady(ctx, srv.URL+"/health", "svc", 30*time.Second)
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

func TestWaitForHTTPReady_2xxVariants(t *testing.T) {
	for _, code := range []int{200, 201, 204, 299} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			s := newTestServerWithHTTPClient(srv.Client())
			if err := s.waitForHTTPReady(context.Background(), srv.URL+"/health", "svc", 5*time.Second); err != nil {
				t.Errorf("status %d should be treated as ready, got error: %v", code, err)
			}
		})
	}
}

// ---- resolveHealthURL ----

func TestResolveHealthURL_NoPlaceholder(t *testing.T) {
	s := newTestServer()
	s.backend = &mockBackend{upstreamHost: "172.17.0.3"}
	got, err := s.resolveHealthURL(context.Background(), "http://myapp:8080/health", "ctr-1", "myapp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://myapp:8080/health" {
		t.Errorf("got %q, want URL unchanged when no placeholder", "http://myapp:8080/health")
	}
}

func TestResolveHealthURL_PlaceholderReplacedWithIP(t *testing.T) {
	s := newTestServer()
	s.backend = &mockBackend{upstreamHost: "172.17.0.3"}
	got, err := s.resolveHealthURL(context.Background(), "http://{{container}}:8080/health", "ctr-1", "myapp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http://172.17.0.3:8080/health"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveHealthURL_GetUpstreamHostError(t *testing.T) {
	s := newTestServer()
	s.backend = &mockBackend{upstreamErr: fmt.Errorf("no network")}
	_, err := s.resolveHealthURL(context.Background(), "http://{{container}}:8080/health", "ctr-1", "myapp", "")
	if err == nil {
		t.Error("expected error when GetUpstreamHost fails")
	}
}

func TestResolveHealthURL_GetUpstreamHostEmptyHost(t *testing.T) {
	s := newTestServer()
	s.backend = &mockBackend{upstreamHost: "", upstreamErr: nil}
	_, err := s.resolveHealthURL(context.Background(), "http://{{container}}:8080/health", "ctr-1", "myapp", "")
	if err == nil {
		t.Error("expected error when GetUpstreamHost returns empty host")
	}
}

// ---- singleflight deduplication ----

func TestStartGroup_DeduplicatesConcurrentEnsureRunning(t *testing.T) {
	var callCount atomic.Int32
	ready := make(chan struct{})

	s := newTestServer()
	s.backend = &mockBackend{
		startFunc: func(_ string) {
			callCount.Add(1)
			<-ready // block until test releases
		},
	}

	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			_, _, _ = s.startGroup.Do("ctr-1", func() (any, error) {
				return nil, s.backend.EnsureRunning(context.Background(), "ctr-1")
			})
		}()
	}

	// Give goroutines time to pile up inside startGroup.Do before releasing.
	time.Sleep(20 * time.Millisecond)
	close(ready)
	wg.Wait()

	if got := callCount.Load(); got != 1 {
		t.Errorf("EnsureRunning called %d times, want exactly 1", got)
	}
}

// ---- targetInfoEqual — TLS and APIKey ----

func TestTargetInfoEqual_TLSDiffers(t *testing.T) {
	a := types.TargetInfo{Ports: []types.PortMapping{{ListenPort: 9000, TargetPort: 80}}, TLS: false}
	b := types.TargetInfo{Ports: []types.PortMapping{{ListenPort: 9000, TargetPort: 80}}, TLS: true}
	if targetInfoEqual(a, b) {
		t.Error("expected not equal when TLS differs")
	}
}

func TestTargetInfoEqual_APIKeyDiffers(t *testing.T) {
	a := types.TargetInfo{Ports: []types.PortMapping{{ListenPort: 9000, TargetPort: 80}}, APIKey: "a"}
	b := types.TargetInfo{Ports: []types.PortMapping{{ListenPort: 9000, TargetPort: 80}}, APIKey: "b"}
	if targetInfoEqual(a, b) {
		t.Error("expected not equal when APIKey differs")
	}
}

func TestTargetInfoEqual_HTTPSAndAPIKeySame(t *testing.T) {
	a := types.TargetInfo{Ports: []types.PortMapping{{ListenPort: 9000, TargetPort: 80}}, TLS: true, APIKey: "x"}
	b := types.TargetInfo{Ports: []types.PortMapping{{ListenPort: 9000, TargetPort: 80}}, TLS: true, APIKey: "x"}
	if !targetInfoEqual(a, b) {
		t.Error("expected equal when TLS and APIKey are identical")
	}
}

// ---- handleHTTPProxy ----

// pipeConn wires two net.Conn halves together via an in-memory pipe.
func pipeConn() (net.Conn, net.Conn) {
	return net.Pipe()
}

func TestHandleHTTPProxy_CorrectKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upConn, err := net.Dial("tcp", upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial upstream: %v", err)
	}
	defer upConn.Close() //nolint:errcheck

	clientConn, proxyConn := pipeConn()
	defer clientConn.Close() //nolint:errcheck

	ts := &targetState{
		info:   types.TargetInfo{ContainerName: "svc"},
		apiKey: "secret",
	}
	s := newTestServer()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHTTPProxy(proxyConn, upConn, ts)
	}()

	req, _ := http.NewRequest(http.MethodGet, "http://ignored/", nil)
	req.Header.Set("X-API-Key", "secret")
	req.Write(clientConn) //nolint:errcheck

	resp, err := http.ReadResponse(newBufReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}
	clientConn.Close() //nolint:errcheck
	<-done
}

func TestHandleHTTPProxy_MissingKey(t *testing.T) {
	clientConn, proxyConn := pipeConn()
	defer clientConn.Close() //nolint:errcheck

	upClient, upServer := pipeConn()
	defer upClient.Close()  //nolint:errcheck
	defer upServer.Close()  //nolint:errcheck

	ts := &targetState{
		info:   types.TargetInfo{ContainerName: "svc"},
		apiKey: "secret",
	}
	s := newTestServer()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHTTPProxy(proxyConn, upClient, ts)
	}()

	req, _ := http.NewRequest(http.MethodGet, "http://ignored/", nil)
	req.Write(clientConn) //nolint:errcheck

	resp, err := http.ReadResponse(newBufReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", resp.StatusCode)
	}
	<-done
}

func TestHandleHTTPProxy_WrongKey(t *testing.T) {
	clientConn, proxyConn := pipeConn()
	defer clientConn.Close() //nolint:errcheck

	upClient, upServer := pipeConn()
	defer upClient.Close()  //nolint:errcheck
	defer upServer.Close()  //nolint:errcheck

	ts := &targetState{
		info:   types.TargetInfo{ContainerName: "svc"},
		apiKey: "secret",
	}
	s := newTestServer()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHTTPProxy(proxyConn, upClient, ts)
	}()

	req, _ := http.NewRequest(http.MethodGet, "http://ignored/", nil)
	req.Header.Set("X-API-Key", "wrong")
	req.Write(clientConn) //nolint:errcheck

	resp, err := http.ReadResponse(newBufReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", resp.StatusCode)
	}
	<-done
}

func TestHandleHTTPProxy_HeaderStripped(t *testing.T) {
	var received *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upConn, err := net.Dial("tcp", upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial upstream: %v", err)
	}
	defer upConn.Close() //nolint:errcheck

	clientConn, proxyConn := pipeConn()
	defer clientConn.Close() //nolint:errcheck

	ts := &targetState{
		info:   types.TargetInfo{ContainerName: "svc"},
		apiKey: "secret",
	}
	s := newTestServer()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHTTPProxy(proxyConn, upConn, ts)
	}()

	req, _ := http.NewRequest(http.MethodGet, "http://ignored/", nil)
	req.Header.Set("X-API-Key", "secret")
	req.Write(clientConn) //nolint:errcheck

	resp, err := http.ReadResponse(newBufReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	clientConn.Close() //nolint:errcheck
	<-done

	if received != nil && received.Header.Get("X-API-Key") != "" {
		t.Error("X-API-Key header should have been stripped before forwarding")
	}
}

func newBufReader(c net.Conn) *bufio.Reader {
	return bufio.NewReader(c)
}
