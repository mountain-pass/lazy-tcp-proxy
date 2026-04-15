package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/proxy"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/scheduler"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

const (
	defaultPollInterval = 15 * time.Second
	defaultIdleTimeout  = 120 * time.Second
	defaultStartTimeout = 30 * time.Second
)

func resolveIdleTimeout() time.Duration {
	raw := os.Getenv("IDLE_TIMEOUT_SECS")
	if raw == "" {
		return defaultIdleTimeout
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Printf("IDLE_TIMEOUT_SECS=%q is invalid; using default %s", raw, defaultIdleTimeout)
		return defaultIdleTimeout
	}
	return time.Duration(n) * time.Second
}

func resolveStartTimeout() time.Duration {
	raw := os.Getenv("START_TIMEOUT_SECS")
	if raw == "" {
		return defaultStartTimeout
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Printf("START_TIMEOUT_SECS=%q is invalid; using default %s", raw, defaultStartTimeout)
		return defaultStartTimeout
	}
	return time.Duration(n) * time.Second
}

func resolvePollInterval() time.Duration {
	raw := os.Getenv("POLL_INTERVAL_SECS")
	if raw == "" {
		return defaultPollInterval
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Printf("POLL_INTERVAL_SECS=%q is invalid; using default %s", raw, defaultPollInterval)
		return defaultPollInterval
	}
	return time.Duration(n) * time.Second
}

const defaultStatusPort = 8080

func resolveStatusPort() int {
	raw := os.Getenv("STATUS_PORT")
	if raw == "" {
		return defaultStatusPort
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Printf("STATUS_PORT=%q is invalid; using default %d", raw, defaultStatusPort)
		return defaultStatusPort
	}
	return n // 0 means disabled
}

const statusDashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Lazy TCP Proxy – Status</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: system-ui, -apple-system, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      padding: 2rem;
    }
    h1 { font-size: 1.4rem; font-weight: 600; margin-bottom: 0.5rem; color: #f8fafc; }
    #last-updated { font-size: 0.75rem; color: #64748b; margin-bottom: 1.5rem; }
    #error { color: #f87171; margin-bottom: 1rem; font-size: 0.875rem; min-height: 1.2em; }
    #containers { display: flex; flex-direction: column; gap: 0.75rem; }
    .container-card {
      background: #1e2330;
      border: 1px solid #2d3748;
      border-radius: 8px;
      padding: 1rem 1.25rem;
	  max-width: 400px;
	  width: 100%;
	  margin: auto;
	  display: flex;
	  flex-direction: column;
	  gap: 0.75rem;
    }
	.flex { display: flex; align-items: center; gap: 0.5rem; }
    .container-header { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; space-y: 1rem; flex-wrap: wrap; }
    .container-name { font-weight: 600; font-size: 1rem; }
    .status-badge {
      font-size: 0.7rem; font-weight: 700; text-transform: uppercase;
      padding: 2px 8px; border-radius: 999px; letter-spacing: 0.05em;
    }
    .status-up   { background: #166534; color: #4ade80; }
    .status-idle { background: #713f12; color: #fbbf24; }
    .status-down { background: #7f1d1d; color: #f87171; }
    .container-id { font-size: 0.7rem; color: #475569; font-family: monospace; margin-left: auto; }
    .ports { display: flex; flex-wrap: wrap; gap: 0.5rem; justify-content: flex-end; }
    .port-entry {
      font-size: 0.78rem; background: #263044; border: 1px solid #374151;
      border-radius: 4px; padding: 2px 8px; color: #94a3b8;
    }
    .active-conns { color: #60a5fa; font-weight: 600; }
    .empty { color: #475569; font-style: italic; }
  </style>
</head>
<body>
  <h1>Lazy TCP Proxy</h1>
  <div id="last-updated">Loading…</div>
  <div id="error"></div>
  <div id="containers"></div>
  <script>
    const statusRank = { up: 3, idle: 2, down: 1 };

    function statusOf(running, activeConns) {
      if (!running) return 'down';
      return activeConns > 0 ? 'up' : 'idle';
    }

    function esc(s) {
      return String(s)
        .replace(/&/g,'&amp;').replace(/</g,'&lt;')
        .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
    }

    function render(data) {
      const el = document.getElementById('containers');
      if (!data.length) {
        el.innerHTML = '<p class="empty">No containers registered.</p>';
        return;
      }

      // Group by container_id, preserving insertion order (already sorted by server)
      const groups = new Map();
      for (const snap of data) {
        const key = snap.container_id || snap.container_name;
        if (!groups.has(key)) {
          groups.set(key, { name: snap.container_name, id: snap.container_id, ports: [] });
        }
        groups.get(key).ports.push(snap);
      }

      let html = '';
      for (const group of groups.values()) {
        // Overall status: best port status wins (up > idle > down)
        let best = 'down';
        for (const p of group.ports) {
          const s = statusOf(p.running, p.active_conns);
          if (statusRank[s] > statusRank[best]) best = s;
        }

        const portBadges = group.ports.sort((a, b) => a.listen_port - b.listen_port).map(p => {
          return '<a class="port-entry" target="_blank" href="' + window.location.protocol + '//' + window.location.hostname + ':' + p.listen_port + '">:' + p.listen_port + ' (' + p.active_conns + ')' + '</a>';
        }).join('');

        html +=
          '<div class="container-card">' +
            '<div class="container-header">' +
              '<span class="container-name">' + esc(group.name) + '</span>' +
                '<span class="status-badge status-' + best + '">' + best + '</span>' +
            '</div>' +
            '<div class="ports">' + portBadges + '</div>' +
          '</div>';
      }
      el.innerHTML = html;
    }

    async function refresh() {
      try {
        const res = await fetch('/status');
        if (!res.ok) throw new Error('HTTP ' + res.status);
        render(await res.json());
        document.getElementById('error').textContent = '';
      } catch (e) {
        document.getElementById('error').textContent = 'Failed to fetch status: ' + e.message;
      }
      document.getElementById('last-updated').textContent =
        'Last updated: ' + new Date().toLocaleTimeString();
    }

    refresh();
    setInterval(refresh, 2000);
  </script>
</body>
</html>`

func runStatusServer(ctx context.Context, srv *proxy.ProxyServer, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(srv.Snapshot()) //nolint:errcheck
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, statusDashboardHTML) //nolint:errcheck
	})
	hs := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	context.AfterFunc(ctx, func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(shutCtx) //nolint:errcheck
	})
	go func() {
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("status server: %v", err)
		}
	}()
}

// backendManager is the full interface required by main: it covers discovery,
// event watching, the three proxy methods, and shutdown cleanup.
type backendManager interface {
	Discover(ctx context.Context, handler types.TargetHandler) error
	WatchEvents(ctx context.Context, handler types.TargetHandler)
	EnsureRunning(ctx context.Context, targetID string) error
	StopContainer(ctx context.Context, targetID, targetName string) error
	GetUpstreamHost(ctx context.Context, targetID, hint string) (string, error)
	WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error
	Shutdown(ctx context.Context)
}


func main() {
	startTime := time.Now()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("lazy-tcp-proxy starting")

	// Root context cancelled on shutdown signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %s; shutting down", sig)
		cancel()
	}()

	// Select and initialise backend
	mgr, err := resolveBackend()
	if err != nil {
		log.Fatalf("failed to create backend: %v", err)
	}

	// Create the proxy server
	idleTimeout := resolveIdleTimeout()
	if idleTimeout == 0 {
		log.Printf("idle timeout: 0s — containers stop immediately when all connections close (set IDLE_TIMEOUT_SECS to override)")
	} else {
		log.Printf("idle timeout: %s (set IDLE_TIMEOUT_SECS to override)", idleTimeout)
	}
	tick := resolvePollInterval()
	log.Printf("inactivity check interval: %s (set POLL_INTERVAL_SECS to override)", tick)
	startTimeout := resolveStartTimeout()
	log.Printf("UDP start timeout: %s (set START_TIMEOUT_SECS or lazy-tcp-proxy.start-timeout-secs label to override)", startTimeout)
	srv := proxy.NewServer(ctx, mgr, startTime, idleTimeout, tick, startTimeout)

	// Create and wire the cron scheduler (must happen before Discover so that
	// initial targets get their schedules registered).
	sched := scheduler.New(ctx, srv)
	srv.SetScheduler(sched)
	sched.Start()
	defer sched.Stop()

	// Start the HTTP status server
	statusPort := resolveStatusPort()
	if statusPort == 0 {
		log.Println("status server: disabled (STATUS_PORT=0)")
	} else {
		log.Printf("status server: listening on :%d (set STATUS_PORT=0 to disable)", statusPort)
		runStatusServer(ctx, srv, statusPort)
	}

	// Initial discovery of all matching targets
	log.Println("performing initial target discovery...")
	if err := mgr.Discover(ctx, srv); err != nil {
		log.Printf("initial discovery error: %v", err)
	}

	// Watch for runtime changes
	go func() {
		mgr.WatchEvents(ctx, srv)
	}()

	// Periodically stop idle targets
	go func() {
		srv.RunInactivityChecker(ctx, tick)
	}()

	log.Println("lazy-tcp-proxy running; waiting for shutdown signal")
	<-ctx.Done()
	log.Println("lazy-tcp-proxy shutting down")
	mgr.Shutdown(context.Background())
	log.Println("lazy-tcp-proxy stopped")
}
