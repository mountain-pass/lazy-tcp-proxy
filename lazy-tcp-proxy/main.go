package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/admin"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/config"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/metrics"
	portainerpkg "github.com/mountain-pass/lazy-tcp-proxy/internal/portainer"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/proxy"
	"github.com/mountain-pass/lazy-tcp-proxy/internal/scheduler"
	traefikpkg "github.com/mountain-pass/lazy-tcp-proxy/internal/traefik"
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

const defaultListenStartPort = 8000

func resolveListenStartPort() int {
	raw := os.Getenv("LISTEN_START_PORT")
	if raw == "" {
		return defaultListenStartPort
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Printf("LISTEN_START_PORT=%q is invalid; using default %d", raw, defaultListenStartPort)
		return defaultListenStartPort
	}
	return n
}

const defaultWebPort = 8080

func resolveWebPort() int {
	if raw := os.Getenv("WEB_PORT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
		log.Printf("WEB_PORT=%q is invalid; checking STATUS_PORT", raw)
	}
	if raw := os.Getenv("STATUS_PORT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
		log.Printf("STATUS_PORT=%q is invalid; using default %d", raw, defaultWebPort)
	}
	return defaultWebPort
}

func resolveWebHost() string {
	return os.Getenv("WEB_HOST")
}

const defaultAdminPort = 0
const defaultConfigPath = "/etc/lazy-tcp-proxy/config.yaml"

func resolveAdminPort() int {
	raw := os.Getenv("ADMIN_PORT")
	if raw == "" {
		return defaultAdminPort
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Printf("ADMIN_PORT=%q is invalid; using default %d", raw, defaultAdminPort)
		return defaultAdminPort
	}
	return n // 0 means disabled
}

func resolveConfigPath() string {
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		return v
	}
	return defaultConfigPath
}

func resolveMetricsPostgresURL() string {
	raw := os.Getenv("METRICS_POSTGRES_URL")
	if raw == "" {
		log.Println("metrics: disabled (METRICS_POSTGRES_URL not set)")
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		log.Printf("metrics: disabled (invalid METRICS_POSTGRES_URL: %v)", err)
		return ""
	}
	safe := *u
	if safe.User != nil {
		safe.User = url.UserPassword("***", "***")
	}
	log.Printf("metrics: enabled (host=%s db=%s)", u.Hostname(), strings.TrimPrefix(u.Path, "/"))
	return raw
}

func resolveComposeDir(configPath string) string {
	if v := os.Getenv("COMPOSE_DIR"); v != "" {
		return v
	}
	return filepath.Join(filepath.Dir(configPath), "compose")
}

func resolveRecipesDir(configPath string) string {
	if v := os.Getenv("RECIPES_DIR"); v != "" {
		return v
	}
	return filepath.Join(filepath.Dir(configPath), "recipes")
}

func ensureDir(path string) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		log.Printf("warning: could not create directory %q: %v", path, err)
	}
}

func resolveTraefikProxyHost() string {
	if v := os.Getenv("TRAEFIK_PROXY_HOST"); v != "" {
		return v
	}
	return "lazy-tcp-proxy"
}

func resolveTraefikEntryPoint() string {
	if v, ok := os.LookupEnv("TRAEFIK_ENTRYPOINT"); ok {
		return v
	}
	return "websecure"
}

func resolveTraefikCertResolver() string {
	if v, ok := os.LookupEnv("TRAEFIK_CERTRESOLVER"); ok {
		return v
	}
	return "myresolver"
}

func resolveTraefikTransportTimeouts() *traefikpkg.TransportTimeouts {
	dial := os.Getenv("TRAEFIK_DIAL_TIMEOUT")
	if dial == "" {
		dial = "30s"
	}
	rht := os.Getenv("TRAEFIK_RESPONSE_HEADER_TIMEOUT")
	if rht == "" {
		rht = "15m"
	}
	idle := os.Getenv("TRAEFIK_IDLE_CONN_TIMEOUT")
	if idle == "" {
		idle = "90s"
	}
	return &traefikpkg.TransportTimeouts{
		DialTimeout:           dial,
		ResponseHeaderTimeout: rht,
		IdleConnTimeout:       idle,
	}
}

//go:embed html/dist/index.html
var statusDashboardHTML string

func resolveCORSAllowOrigins() string {
	v := os.Getenv("CORS_ALLOW_ORIGINS")
	if v == "" {
		log.Println("CORS: disabled (CORS_ALLOW_ORIGINS not set)")
	} else {
		log.Printf("CORS: enabled (Access-Control-Allow-Origin: %s)", v)
	}
	return v
}

func corsMiddleware(origins string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origins)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func runStatusServer(ctx context.Context, srv *proxy.ProxyServer, mgr backendManager, metricsCollector **metrics.Collector, port int, traefikProxyHost, traefikEntryPoint, traefikCertResolver, webHost string, webPort int, traefikTimeouts *traefikpkg.TransportTimeouts, recipesDir, corsOrigins string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		var memUsed, memTotal *int64
		var memContainers *[]types.ContainerMemoryStat
		if used, total, perContainer, err := mgr.ContainerMemoryStats(r.Context()); err == nil {
			memUsed = &used
			memTotal = &total
			if len(perContainer) > 0 {
				memContainers = &perContainer
			}
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]any{ //nolint:errcheck
			"services":     srv.Snapshot(),
			"memory_used":  memUsed,
			"memory_total": memTotal,
			"containers":   memContainers,
		})
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		col := *metricsCollector
		if col == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "metrics not configured"}) //nolint:errcheck
			return
		}
		rows, err := col.HourlyActivity(r.Context())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
			return
		}
		if rows == nil {
			rows = []metrics.ServiceActivity{}
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(rows) //nolint:errcheck
	})
	mux.HandleFunc("/traefik", func(w http.ResponseWriter, r *http.Request) {
		raw := srv.Snapshot()
		snaps := make([]traefikpkg.Snapshot, len(raw))
		for i, s := range raw {
			snaps[i] = traefikpkg.Snapshot{
				ListenPort:      s.ListenPort,
				TraefikHosts:    s.TraefikHosts,
				TraefikTCPHosts: s.TraefikTCPHosts,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(traefikpkg.BuildConfig(snaps, traefikProxyHost, traefikEntryPoint, traefikCertResolver, webHost, webPort, traefikTimeouts)) //nolint:errcheck
	})
	portainerBaseURL := fmt.Sprintf("http://%s:%d", traefikProxyHost, webPort)
	mux.HandleFunc("/portainer/templates", portainerpkg.TemplatesHandler(recipesDir, portainerBaseURL))
	mux.Handle("/portainer/git/", portainerpkg.GitHandler(recipesDir))
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
	var handler http.Handler = mux
	if corsOrigins != "" {
		handler = corsMiddleware(corsOrigins, mux)
	}
	hs := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: handler}
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
	DiscoverServices(ctx context.Context, handler types.TargetHandler) error
	WatchEvents(ctx context.Context, handler types.TargetHandler)
	WatchServiceEvents(ctx context.Context, handler types.TargetHandler)
	EnsureRunning(ctx context.Context, targetID string) error
	StopContainer(ctx context.Context, targetID, targetName string) error
	GetUpstreamHost(ctx context.Context, targetID, hint string) (string, error)
	WaitUntilHealthy(ctx context.Context, containerID, name string, timeout time.Duration) error
	Shutdown(ctx context.Context)
	// DefaultTargetID returns a backend-appropriate container ID for a given
	// name. Used by the config store to assign IDs to YAML-only targets.
	// Docker: returns name as-is (Docker API accepts names). K8s: returns "namespace/name".
	DefaultTargetID(name string) string
	// NotifyTargets syncs internal backend state (e.g. swarm service registry)
	// from the merged target list after config overlay is applied.
	NotifyTargets(targets []types.TargetInfo)
	// JoinNetworksForContainerNames inspects each named container and joins its
	// Docker networks. Used to connect the proxy to networks for config-only
	// targets that are not discovered via Docker labels. No-op on Kubernetes.
	JoinNetworksForContainerNames(ctx context.Context, names []string)
	// InspectRunning reports whether the container/service identified by targetID
	// is currently running and whether it exists at all. Used to populate the
	// Running and Missing fields for YAML-only targets not discovered via backend
	// labels. Returns (false, true, nil) on backends where this is not applicable
	// (e.g. Kubernetes). Returns (false, false, nil) when the container is absent.
	InspectRunning(ctx context.Context, targetID string) (running, exists bool, err error)
	// SetConfigOnlyNames registers the name→registeredContainerID mapping for
	// containers that are managed via config.yaml only (no backend label).
	// The Docker backend uses this so WatchEvents can route start/stop events
	// for unlabelled containers. No-op on Kubernetes.
	SetConfigOnlyNames(nameToID map[string]string)
	// SetComposeDir sets the directory scanned for compose files and image archives
	// when re-provisioning a missing container. No-op on Kubernetes.
	SetComposeDir(dir string)
	// SetPortAllocator wires the dynamic port allocator used to assign listen ports
	// to traefik_hosts / traefik_tcp_hosts label entries.
	SetPortAllocator(a *types.PortAllocator)
	// ContainerMemoryStats returns the aggregate memory used by all running
	// containers, the host's total physical memory, and a per-container
	// breakdown, all in bytes. Returns (0, 0, nil, nil) on backends where this
	// is not applicable (e.g. Kubernetes).
	ContainerMemoryStats(ctx context.Context) (used, total int64, containers []types.ContainerMemoryStat, err error)
}

// discoverAndApply runs backend discovery, applies the YAML config overlay,
// and updates the proxy server with the merged target list.
func discoverAndApply(ctx context.Context, mgr backendManager, store *config.Store, srv *proxy.ProxyServer) error {
	collector := &config.TargetCollector{}
	if err := mgr.Discover(ctx, collector); err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	if err := mgr.DiscoverServices(ctx, collector); err != nil {
		log.Printf("discover services warning: %v", err)
	}

	discovered := collector.Targets()
	merged, errs := store.Apply(discovered, mgr.DefaultTargetID)
	for _, e := range errs {
		log.Printf("config apply warning: %v", e)
	}

	// Build set of ContainerIDs that came from label discovery.
	discoveredIDSet := make(map[string]bool, len(discovered))
	for _, t := range discovered {
		discoveredIDSet[t.ContainerID] = true
	}

	// For YAML-only entries (not in the discovered set), inspect the actual
	// container running state and build the name→ID map for event routing.
	configOnlyNameToID := make(map[string]string)
	for i, t := range merged {
		if !discoveredIDSet[t.ContainerID] {
			running, exists, err := mgr.InspectRunning(ctx, t.ContainerID)
			if err != nil {
				log.Printf("config: could not inspect running state for %q: %v", t.ContainerName, err)
			} else {
				merged[i].Running = running
				merged[i].Missing = !exists
				if !exists {
					log.Printf("config: container \033[33m%s\033[0m not found — marking as missing", t.ContainerName)
				}
			}
			configOnlyNameToID[t.ContainerName] = t.ContainerID
		}
	}
	mgr.SetConfigOnlyNames(configOnlyNameToID)

	mgr.NotifyTargets(merged)

	var configOnlyNames []string
	for _, t := range merged {
		if len(t.NetworkIDs) == 0 {
			configOnlyNames = append(configOnlyNames, t.ContainerName)
		}
	}
	mgr.JoinNetworksForContainerNames(ctx, configOnlyNames)

	srv.Update(merged)
	return nil
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
	tlsConfig, tlsErr := proxy.GenerateSelfSignedTLSConfig()
	if tlsErr != nil {
		log.Fatalf("failed to generate self-signed TLS certificate: %v", tlsErr)
	}
	srv := proxy.NewServer(ctx, mgr, startTime, idleTimeout, tick, startTimeout, tlsConfig)

	// Create and wire the cron scheduler (must happen before Discover so that
	// initial targets get their schedules registered).
	sched := scheduler.New(ctx, srv)
	srv.SetScheduler(sched)
	sched.Start()
	defer sched.Stop()

	// Resolve config and directory paths up front so both the web server and
	// compose re-provisioning use consistent absolute paths.
	configPath := resolveConfigPath()
	recipesDir := resolveRecipesDir(configPath)
	ensureDir(recipesDir)
	log.Printf("portainer: GET /portainer/templates and git clone /portainer/git available (RECIPES_DIR=%s)", recipesDir)

	// metricsCollector is nil until PostgreSQL is connected (after network join).
	// The HTTP server holds a pointer-to-pointer so it sees the update without locking.
	var metricsCollector *metrics.Collector

	// Start the HTTP status server
	webPort := resolveWebPort()
	webHost := resolveWebHost()
	traefikProxyHost := resolveTraefikProxyHost()
	traefikEntryPoint := resolveTraefikEntryPoint()
	traefikCertResolver := resolveTraefikCertResolver()
	traefikTimeouts := resolveTraefikTransportTimeouts()
	if webPort == 0 {
		log.Println("web server: disabled (WEB_PORT=0)")
	} else {
		if webHost != "" {
			log.Printf("web server: listening on :%d (WEB_HOST=%s)", webPort, webHost)
		} else {
			log.Printf("web server: listening on :%d (WEB_HOST not set — no Traefik route for web endpoint)", webPort)
		}
		log.Printf("traefik provider: GET /traefik available (TRAEFIK_PROXY_HOST=%s, TRAEFIK_ENTRYPOINT=%q, TRAEFIK_CERTRESOLVER=%q, TRAEFIK_DIAL_TIMEOUT=%s, TRAEFIK_RESPONSE_HEADER_TIMEOUT=%s, TRAEFIK_IDLE_CONN_TIMEOUT=%s)",
			traefikProxyHost, traefikEntryPoint, traefikCertResolver,
			traefikTimeouts.DialTimeout, traefikTimeouts.ResponseHeaderTimeout, traefikTimeouts.IdleConnTimeout)
		runStatusServer(ctx, srv, mgr, &metricsCollector, webPort, traefikProxyHost, traefikEntryPoint, traefikCertResolver, webHost, webPort, traefikTimeouts, recipesDir, resolveCORSAllowOrigins())
	}

	// Load dynamic config file
	composeDir := resolveComposeDir(configPath)
	ensureDir(composeDir)
	mgr.SetComposeDir(composeDir)
	srv.SetComposeDir(composeDir)
	log.Printf("compose dir: %s (set COMPOSE_DIR to override)", composeDir)
	listenStartPort := resolveListenStartPort()
	log.Printf("traefik host port allocator: starting at port %d (set LISTEN_START_PORT to override)", listenStartPort)
	portAlloc := types.NewPortAllocator(listenStartPort)
	mgr.SetPortAllocator(portAlloc)

	store := config.New(configPath)
	store.SetPortAllocator(portAlloc)
	if err := store.Load(); err != nil {
		log.Fatalf("failed to load config file: %v", err)
	}
	log.Printf("config: loaded from %s (%d services)", configPath, len(store.Get().Services))

	// Start the admin server (if enabled)
	adminPort := resolveAdminPort()
	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminPort == 0 {
		log.Println("admin server: disabled (ADMIN_PORT=0)")
	} else {
		if adminKey == "" {
			log.Fatal("ADMIN_API_KEY must be set when ADMIN_PORT is non-zero")
		}
		reloadFn := func(ctx context.Context) error {
			return discoverAndApply(ctx, mgr, store, srv)
		}
		adminSrv := admin.New(store, reloadFn, adminKey)
		go adminSrv.Run(ctx, adminPort)
	}

	// Initial discovery of all matching targets (with config overlay applied)
	log.Println("performing initial target discovery...")
	if err := discoverAndApply(ctx, mgr, store, srv); err != nil {
		log.Printf("initial discovery error: %v", err)
	}

	// Initialise metrics collector after network join and target registration so
	// a locally hosted PostgreSQL container is reachable via its Docker network.
	// SetCollector back-fills all already-registered ports automatically.
	if pgURL := resolveMetricsPostgresURL(); pgURL != "" {
		log.Printf("metrics: connecting to PostgreSQL...")
		collector, err := metrics.New(ctx, pgURL)
		if err != nil {
			log.Printf("metrics: failed to connect to PostgreSQL (%v); metrics disabled", err)
		} else {
			log.Printf("metrics: connected to PostgreSQL successfully")
			metricsCollector = collector
			srv.SetCollector(collector)
			go collector.Run(ctx)
		}
	}

	// Watch for runtime changes (WatchEvents/WatchServiceEvents call RegisterTarget
	// directly for label-carrying containers/services; config overlay applied on reload only).
	go func() {
		mgr.WatchEvents(ctx, srv)
	}()
	go func() {
		mgr.WatchServiceEvents(ctx, srv)
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
