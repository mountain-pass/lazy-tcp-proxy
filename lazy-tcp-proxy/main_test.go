package main

import (
	"testing"
	"time"
)

func TestResolveIdleTimeout_Default(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT_SECS", "")
	if got := resolveIdleTimeout(); got != defaultIdleTimeout {
		t.Errorf("got %s, want %s", got, defaultIdleTimeout)
	}
}

func TestResolveIdleTimeout_ValidValue(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT_SECS", "60")
	if got := resolveIdleTimeout(); got != 60*time.Second {
		t.Errorf("got %s, want 60s", got)
	}
}

func TestResolveIdleTimeout_ZeroMeansImmediate(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT_SECS", "0")
	if got := resolveIdleTimeout(); got != 0 {
		t.Errorf("got %s, want 0s (immediate shutdown)", got)
	}
}

func TestResolveIdleTimeout_NegativeFallsBackToDefault(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT_SECS", "-5")
	if got := resolveIdleTimeout(); got != defaultIdleTimeout {
		t.Errorf("got %s, want default %s", got, defaultIdleTimeout)
	}
}

func TestResolveIdleTimeout_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT_SECS", "notanumber")
	if got := resolveIdleTimeout(); got != defaultIdleTimeout {
		t.Errorf("got %s, want default %s", got, defaultIdleTimeout)
	}
}

func TestResolvePollInterval_Default(t *testing.T) {
	t.Setenv("POLL_INTERVAL_SECS", "")
	if got := resolvePollInterval(); got != defaultPollInterval {
		t.Errorf("got %s, want %s", got, defaultPollInterval)
	}
}

func TestResolvePollInterval_ValidValue(t *testing.T) {
	t.Setenv("POLL_INTERVAL_SECS", "30")
	if got := resolvePollInterval(); got != 30*time.Second {
		t.Errorf("got %s, want 30s", got)
	}
}

func TestResolvePollInterval_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("POLL_INTERVAL_SECS", "bad")
	if got := resolvePollInterval(); got != defaultPollInterval {
		t.Errorf("got %s, want default %s", got, defaultPollInterval)
	}
}

func TestResolveWebPort_Default(t *testing.T) {
	t.Setenv("WEB_PORT", "")
	t.Setenv("STATUS_PORT", "")
	if got := resolveWebPort(); got != defaultWebPort {
		t.Errorf("got %d, want %d", got, defaultWebPort)
	}
}

func TestResolveWebPort_ValidValue(t *testing.T) {
	t.Setenv("WEB_PORT", "9090")
	t.Setenv("STATUS_PORT", "")
	if got := resolveWebPort(); got != 9090 {
		t.Errorf("got %d, want 9090", got)
	}
}

func TestResolveWebPort_ZeroDisables(t *testing.T) {
	t.Setenv("WEB_PORT", "0")
	t.Setenv("STATUS_PORT", "")
	if got := resolveWebPort(); got != 0 {
		t.Errorf("got %d, want 0 (disabled)", got)
	}
}

func TestResolveWebPort_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("WEB_PORT", "notaport")
	t.Setenv("STATUS_PORT", "")
	if got := resolveWebPort(); got != defaultWebPort {
		t.Errorf("got %d, want default %d", got, defaultWebPort)
	}
}

func TestResolveWebPort_NegativeFallsBackToDefault(t *testing.T) {
	t.Setenv("WEB_PORT", "-1")
	t.Setenv("STATUS_PORT", "")
	if got := resolveWebPort(); got != defaultWebPort {
		t.Errorf("got %d, want default %d", got, defaultWebPort)
	}
}

func TestResolveWebPort_FallsBackToStatusPort(t *testing.T) {
	t.Setenv("WEB_PORT", "")
	t.Setenv("STATUS_PORT", "9091")
	if got := resolveWebPort(); got != 9091 {
		t.Errorf("got %d, want 9091", got)
	}
}
