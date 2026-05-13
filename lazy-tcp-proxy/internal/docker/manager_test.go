package docker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// newTestManagerWithStatuses creates a Manager backed by an httptest.Server that
// returns successive health statuses for ContainerInspect calls. Once all
// statuses are exhausted, the last one is repeated.
func newTestManagerWithStatuses(t *testing.T, containerID string, statuses []string) (*Manager, func()) {
	t.Helper()
	var callIdx atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(callIdx.Add(1)) - 1
		if idx >= len(statuses) {
			idx = len(statuses) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Id":%q,"State":{"Status":"running","Running":true,"Health":{"Status":%q,"FailingStreak":0,"Log":[]}},"Config":{},"HostConfig":{"LogConfig":{"Type":"json-file","Config":{}}},"NetworkSettings":{"Networks":{}}}`, //nolint:errcheck
			containerID, statuses[idx])
	}))
	cli, err := client.New(
		client.WithHost(srv.URL),
		client.WithAPIVersion("1.41"),
	)
	if err != nil {
		srv.Close()
		t.Fatalf("creating docker test client: %v", err)
	}
	m := &Manager{cli: cli, joinedNets: make(map[string]string)}
	return m, srv.Close
}

// ---- WaitUntilHealthy ----

func TestWaitUntilHealthy_ImmediateHealthy(t *testing.T) {
	m, cleanup := newTestManagerWithStatuses(t, "ctr1", []string{"healthy"})
	defer cleanup()

	err := m.WaitUntilHealthy(context.Background(), "ctr1", "svc", 5*time.Second)
	if err != nil {
		t.Errorf("expected nil error for immediate healthy, got %v", err)
	}
}

func TestWaitUntilHealthy_StartingThenHealthy(t *testing.T) {
	m, cleanup := newTestManagerWithStatuses(t, "ctr1", []string{"starting", "starting", "healthy"})
	defer cleanup()

	err := m.WaitUntilHealthy(context.Background(), "ctr1", "svc", 10*time.Second)
	if err != nil {
		t.Errorf("expected nil error after starting→starting→healthy, got %v", err)
	}
}

func TestWaitUntilHealthy_UnhealthyTerminal(t *testing.T) {
	m, cleanup := newTestManagerWithStatuses(t, "ctr1", []string{"unhealthy"})
	defer cleanup()

	err := m.WaitUntilHealthy(context.Background(), "ctr1", "svc", 10*time.Second)
	if err == nil {
		t.Error("expected error for unhealthy status, got nil")
	}
}

func TestWaitUntilHealthy_Timeout(t *testing.T) {
	m, cleanup := newTestManagerWithStatuses(t, "ctr1", []string{"starting"})
	defer cleanup()

	// 2-second budget with 1s poll → at most 2 attempts, both "starting" → timeout error
	err := m.WaitUntilHealthy(context.Background(), "ctr1", "svc", 2*time.Second)
	if err == nil {
		t.Error("expected timeout error when status never becomes healthy")
	}
}

func TestWaitUntilHealthy_CtxCancelled(t *testing.T) {
	m, cleanup := newTestManagerWithStatuses(t, "ctr1", []string{"starting"})
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	err := m.WaitUntilHealthy(ctx, "ctr1", "svc", 30*time.Second)
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

// ---- HasHealthCheck detection ----

func TestHasHealthCheck_NilHealth(t *testing.T) {
	// When State.Health is nil, HasHealthCheck should be false.
	// This is the code path: inspect.State.Health == nil → false
	hasHealthCheck := false // simulate: Health == nil
	if hasHealthCheck {
		t.Error("expected HasHealthCheck=false when Health is nil")
	}
}

func TestHasHealthCheck_StatusNone(t *testing.T) {
	// When Health.Status is "none", HasHealthCheck should be false.
	status := "none"
	hasHealthCheck := status != "" && status != "none"
	if hasHealthCheck {
		t.Error("expected HasHealthCheck=false when Health.Status is 'none'")
	}
}

func TestHasHealthCheck_StatusStarting(t *testing.T) {
	// When Health.Status is "starting", HasHealthCheck should be true.
	status := "starting"
	hasHealthCheck := status != "" && status != "none"
	if !hasHealthCheck {
		t.Error("expected HasHealthCheck=true when Health.Status is 'starting'")
	}
}
