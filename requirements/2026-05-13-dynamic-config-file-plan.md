# Dynamic Configuration File (YAML Override Store) — Implementation Plan

**Requirement**: [2026-05-13-dynamic-config-file.md](2026-05-13-dynamic-config-file.md)
**Date**: 2026-05-13
**Status**: Draft

---

## Overview

This plan adds two new capabilities:

1. A **YAML config file** (`CONFIG_PATH`) that overrides or supplements backend-discovered targets.
2. An **admin HTTP server** (`ADMIN_PORT`) protected by an API key (`ADMIN_API_KEY`) with three
   endpoints: `GET /config`, `GET /config/reload`, `PUT /config/update`.

---

## Implementation Steps

### Step 1 — Create `internal/config/store.go`

New file. Defines the YAML schema, the `Store` type, and the `Apply()` merge logic.

**Key types:**

```go
// DynamicConfig is the top-level YAML (and JSON) structure.
type DynamicConfig struct {
    Services []ServiceEntry `yaml:"services" json:"services"`
}

// ServiceEntry mirrors every configurable field of types.TargetInfo.
type ServiceEntry struct {
    Name             string   `yaml:"name"              json:"name"`
    Ports            []string `yaml:"ports,omitempty"   json:"ports,omitempty"`
    UDPPorts         []string `yaml:"udp_ports,omitempty" json:"udp_ports,omitempty"`
    AllowList        []string `yaml:"allow_list,omitempty" json:"allow_list,omitempty"`
    BlockList        []string `yaml:"block_list,omitempty" json:"block_list,omitempty"`
    IdleTimeoutSecs  *int     `yaml:"idle_timeout_secs,omitempty"  json:"idle_timeout_secs,omitempty"`
    StartTimeoutSecs *int     `yaml:"start_timeout_secs,omitempty" json:"start_timeout_secs,omitempty"`
    WebhookURL       string   `yaml:"webhook_url,omitempty"       json:"webhook_url,omitempty"`
    Dependants       []string `yaml:"dependants,omitempty"        json:"dependants,omitempty"`
    CronStart        string   `yaml:"cron_start,omitempty"        json:"cron_start,omitempty"`
    CronStop         string   `yaml:"cron_stop,omitempty"         json:"cron_stop,omitempty"`
    HTTPHealthCheck  string   `yaml:"http_healthcheck,omitempty"  json:"http_healthcheck,omitempty"`
}

// Store holds the loaded config and the file path.
type Store struct {
    mu     sync.RWMutex
    path   string
    config DynamicConfig
}
```

**Key methods on `Store`:**

- `New(path string) *Store` — constructor; does NOT load yet.

- `Load() error` — reads the file at `s.path`, YAML-decodes into `s.config`. If the file does
  not exist, writes an empty `services: []\n` placeholder, logs a `slog.Warn`, and returns nil.

- `Save(cfg DynamicConfig) error` — validates `cfg` (see `validate()`), YAML-encodes `cfg`,
  writes atomically (write to `path.tmp` then rename), then sets `s.config = cfg`.

- `Get() DynamicConfig` — returns a copy of `s.config` under read-lock.

- `Apply(discovered []types.TargetInfo, defaultID func(string) string) ([]types.TargetInfo, []error)` —
  see merge logic below.

- `validate(cfg DynamicConfig) []error` — runs per-entry validation; collects all errors
  (non-fatal per entry) rather than stopping at first failure.

**`Apply()` merge logic:**

```
1. Build name → index map from discovered slice.
2. Start result slice as a copy of discovered.
3. For each ServiceEntry in s.config.Services:
   a. Call entryToTargetInfo(entry) — returns (TargetInfo, error).
      On error: append to errs, skip entry, continue.
   b. If entry.Name matches a discovered target (by ContainerName):
      - Keep ContainerID, NetworkIDs, Running, HasHealthCheck from discovered.
      - Replace all other fields with YAML-derived values.
      - Overwrite result[idx].
   c. Else (YAML-only entry):
      - Set info.ContainerID = defaultID(entry.Name).
      - Append info to result.
4. Return result, errs.
```

**`entryToTargetInfo()`** — converts `ServiceEntry` → `types.TargetInfo`:
- Port strings (e.g. `"9000:80"`) joined with `,` and passed to `types.ParsePortMappings()`.
- IP lists joined with `,` and passed to `types.ParseIPList()`.
- `IdleTimeoutSecs` / `StartTimeoutSecs` converted to `*time.Duration`.
- `WebhookURL` validated via `url.ParseRequestURI`.
- `CronStart` / `CronStop` validated via `cron.ParseStandard` (import `github.com/robfig/cron/v3`).
- Returns error if both `Ports` and `UDPPorts` are empty after parsing.

**`TargetCollector`** (also in this package) — a `types.TargetHandler` implementation that
collects targets into a slice instead of registering them with the proxy. Used during reload:

```go
type TargetCollector struct {
    mu      sync.Mutex
    targets []types.TargetInfo
}
func (c *TargetCollector) RegisterTarget(info types.TargetInfo) { /* append */ }
func (c *TargetCollector) RemoveTarget(_ string)                {}
func (c *TargetCollector) ContainerStopped(_ string)            {}
func (c *TargetCollector) ContainerStarted(_ string)            {}
func (c *TargetCollector) Targets() []types.TargetInfo          { /* return copy */ }
```

---

### Step 2 — Add `DefaultTargetID` to `backendManager` interface in `main.go`

In `main.go` at the `backendManager` interface definition (lines ~245–253), add one method:

```go
// DefaultTargetID returns the backend-appropriate container ID for a given name.
// Docker: returns name as-is (Docker API accepts names as IDs).
// Kubernetes: returns "namespace/name".
DefaultTargetID(name string) string
```

---

### Step 3 — Implement `DefaultTargetID` on Docker manager

In `internal/docker/manager.go`, add:

```go
func (m *Manager) DefaultTargetID(name string) string { return name }
```

Docker's API (`ContainerInspect`, `ContainerStart`, `ContainerStop`) accepts container names
as IDs, so no lookup is needed.

---

### Step 4 — Implement `DefaultTargetID` on Kubernetes backend

In `internal/k8s/backend.go`, add:

```go
func (b *Backend) DefaultTargetID(name string) string {
    ns := b.namespace
    if ns == "" {
        ns = "default"
    }
    return ns + "/" + name
}
```

---

### Step 5 — Add `Update()` to `ProxyServer`

In `internal/proxy/server.go`, add two methods:

**`currentTargetsByID() map[string]types.TargetInfo`** — returns a snapshot of all currently
registered targets keyed by ContainerID (one entry per container; multiple ports share one ID):

```go
func (s *ProxyServer) currentTargetsByID() map[string]types.TargetInfo {
    s.mu.RLock()
    defer s.mu.RUnlock()
    out := make(map[string]types.TargetInfo)
    for _, ts := range s.targets {
        out[ts.info.ContainerID] = ts.info
    }
    for _, us := range s.udpTargets {
        out[us.info.ContainerID] = us.info
    }
    return out
}
```

**`Update(newTargets []types.TargetInfo)`** — reconciles the proxy's registered targets with
the given list. Uses ContainerID as the reconciliation key:

```go
func (s *ProxyServer) Update(newTargets []types.TargetInfo) {
    current := s.currentTargetsByID()

    newByID := make(map[string]types.TargetInfo, len(newTargets))
    for _, t := range newTargets {
        newByID[t.ContainerID] = t
    }

    // Remove targets no longer present.
    for id := range current {
        if _, ok := newByID[id]; !ok {
            s.RemoveTarget(id)
        }
    }

    // Add new targets; re-register changed ones.
    for id, newInfo := range newByID {
        if cur, exists := current[id]; !exists {
            s.RegisterTarget(newInfo)
        } else if !targetInfoEqual(cur, newInfo) {
            s.RemoveTarget(id)
            s.RegisterTarget(newInfo)
        }
    }
}
```

**`targetInfoEqual(a, b types.TargetInfo) bool`** — compares all mutable fields (Ports,
UDPPorts, AllowList, BlockList, IdleTimeout, StartTimeout, WebhookURL, Dependants, CronStart,
CronStop, HTTPHealthCheck). Does NOT compare ContainerID, NetworkIDs, Running, or HasHealthCheck
(backend-managed fields). Uses `reflect.DeepEqual` for slice/pointer fields.

---

### Step 6 — Create `internal/admin/server.go`

New file. Defines the admin HTTP server.

```go
type Server struct {
    store   *config.Store
    reload  func(ctx context.Context) error   // injected from main
    proxy   snapshotProvider
    apiKey  string
}

type snapshotProvider interface {
    Snapshot() []proxy.TargetSnapshot
}
```

**Constructor:**

```go
func New(store *config.Store, reload func(context.Context) error,
         proxy snapshotProvider, apiKey string) *Server
```

**`Run(ctx context.Context, port int)`** — starts the HTTP server; shuts down gracefully on
context cancellation (mirrors the pattern in `main.go:runStatusServer`, lines 210–241):

```go
func (s *Server) Run(ctx context.Context, port int) {
    mux := http.NewServeMux()
    mux.Handle("GET /config",         s.auth(s.handleGetConfig))
    mux.Handle("GET /config/reload",  s.auth(s.handleReload))
    mux.Handle("PUT /config/update",  s.auth(s.handleUpdate))
    // start listener, context cancel → Shutdown with 5s timeout
}
```

**`auth(next http.HandlerFunc) http.Handler`** — middleware that checks the `X-API-Key` header
against `s.apiKey`. Returns `401 {"error":"unauthorized"}` on mismatch.

**`handleGetConfig`** — marshals `s.store.Get()` to JSON; responds 200.

**`handleReload`** — calls `s.reload(r.Context())`; responds with:
```json
{ "status": "ok", "services_loaded": N }
```
On error: `500 {"status":"error","error":"..."}`.

**`handleUpdate`** — decodes JSON body into `DynamicConfig`; calls `s.store.Save(cfg)`; on
success calls `s.reload(r.Context())`; returns same shape as reload. On decode/validation
error: `400 {"status":"error","error":"..."}` and does NOT modify the file.

---

### Step 7 — Wire everything in `main.go`

**7a. New env var resolvers** (add alongside existing resolver functions):

```go
func resolveConfigPath() string {
    if v := os.Getenv("CONFIG_PATH"); v != "" {
        return v
    }
    return "/etc/lazy-tcp-proxy/config.yaml"
}

func resolveAdminPort() int   // same pattern as resolveStatusPort(); default 8081
func resolveAdminAPIKey() string { return os.Getenv("ADMIN_API_KEY") }
```

**7b. Update `backendManager` interface** — add `DefaultTargetID(name string) string`
(Step 2 above).

**7c. Extract `discoverAndApply` helper:**

```go
func discoverAndApply(ctx context.Context, mgr backendManager,
                      store *config.Store, srv *proxy.ProxyServer) error {
    collector := &config.TargetCollector{}
    if err := mgr.Discover(ctx, collector); err != nil {
        return fmt.Errorf("discover: %w", err)
    }
    merged, errs := store.Apply(collector.Targets(), mgr.DefaultTargetID)
    for _, e := range errs {
        slog.Warn("config apply warning", "err", e)
    }
    srv.Update(merged)
    return nil
}
```

**7d. In `main()`**, replace the existing `mgr.Discover(ctx, srv)` call (line ~310) with:

```go
configPath := resolveConfigPath()
store := config.New(configPath)
if err := store.Load(); err != nil {
    slog.Error("failed to load config", "path", configPath, "err", err)
    os.Exit(1)
}

if err := discoverAndApply(ctx, mgr, store, srv); err != nil {
    slog.Error("initial discover failed", "err", err)
    os.Exit(1)
}
```

Keep the existing `go mgr.WatchEvents(ctx, srv)` call unchanged — WatchEvents continues to
call `srv.RegisterTarget` directly for label-carrying containers; the YAML override only applies
at startup and on explicit reload.

**7e. Admin server startup** (after status server, before inactivity checker):

```go
adminPort := resolveAdminPort()
adminKey  := resolveAdminAPIKey()
if adminPort > 0 {
    if adminKey == "" {
        slog.Error("ADMIN_API_KEY must be set when ADMIN_PORT is non-zero")
        os.Exit(1)
    }
    reloadFn := func(ctx context.Context) error {
        return discoverAndApply(ctx, mgr, store, srv)
    }
    adminSrv := admin.New(store, reloadFn, srv, adminKey)
    go adminSrv.Run(ctx, adminPort)
}
```

**7f. Init log message** — add a log line after store.Load() to record config path and number
of services loaded (mirrors the existing structured init logging pattern).

---

### Step 8 — Promote `gopkg.in/yaml.v3` to direct dependency

```bash
cd lazy-tcp-proxy && go get gopkg.in/yaml.v3
```

This moves `gopkg.in/yaml.v3` from `// indirect` to the direct `require` block and regenerates
`go.sum` if needed.

---

### Step 9 — Update requirement status

Update `requirements/2026-05-13-dynamic-config-file.md`: Status → `In Progress`.
Update `requirements/_index.md` row for REQ-065: Status → `In Progress`.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/config/store.go` | **Create** | YAML store, `DynamicConfig`/`ServiceEntry` structs, `Store.Load/Save/Get/Apply`, `TargetCollector`, `entryToTargetInfo`, `validate` |
| `lazy-tcp-proxy/internal/admin/server.go` | **Create** | Admin HTTP server, auth middleware, `GET /config`, `GET /config/reload`, `PUT /config/update` |
| `lazy-tcp-proxy/internal/proxy/server.go` | **Modify** | Add `Update()`, `currentTargetsByID()`, `targetInfoEqual()` |
| `lazy-tcp-proxy/main.go` | **Modify** | Add `backendManager.DefaultTargetID`, `resolveConfigPath/AdminPort/AdminAPIKey`, `discoverAndApply`, wire `config.Store` and `admin.Server` |
| `lazy-tcp-proxy/internal/docker/manager.go` | **Modify** | Add `DefaultTargetID()` returning `name` |
| `lazy-tcp-proxy/internal/k8s/backend.go` | **Modify** | Add `DefaultTargetID()` returning `namespace/name` |
| `lazy-tcp-proxy/go.mod` | **Modify** | Promote `gopkg.in/yaml.v3` to direct dependency |

---

## API Contracts

### Authentication

All admin endpoints require:
```
X-API-Key: <value of ADMIN_API_KEY env var>
```
Missing or wrong key → `401 Unauthorized`:
```json
{ "error": "unauthorized" }
```

### `GET /config`

Response `200`:
```json
{
  "services": [
    {
      "name": "my-container",
      "ports": ["9000:80"],
      "udp_ports": ["5353:53"],
      "allow_list": ["192.168.0.0/24"],
      "idle_timeout_secs": 60
    }
  ]
}
```

### `GET /config/reload`

Response `200`:
```json
{ "status": "ok", "services_loaded": 2 }
```
Response `500`:
```json
{ "status": "error", "error": "discover: ..." }
```

### `PUT /config/update`

Request body: same JSON shape as `GET /config` response.

Response `200`:
```json
{ "status": "ok", "services_loaded": 2 }
```
Response `400` (parse/validation error — file NOT modified):
```json
{ "status": "error", "error": "service \"foo\": must specify at least one port" }
```

---

## Data Models

### Config file on disk

```yaml
# /etc/lazy-tcp-proxy/config.yaml  (default path; override with CONFIG_PATH)
services:
  - name: "my-container"
    ports:
      - "9000:80"
    udp_ports:
      - "5353:53"
    allow_list:
      - "192.168.0.0/24"
    block_list:
      - "10.0.0.1"
    idle_timeout_secs: 60
    start_timeout_secs: 30
    webhook_url: "https://example.com/hook"
    dependants:
      - "other-service"
    cron_start: "0 9 * * 1-5"
    cron_stop:  "0 17 * * 1-5"
    http_healthcheck: "http://{{container}}:8080/health"
```

### Merge logic (Apply)

```
discovered (from backend)   +   YAML config   =   merged (passed to srv.Update)

  containerA (labels)       │   containerA       →  containerA (YAML wins entirely)
  containerB (labels)       │   (no entry)       →  containerB (labels unchanged)
  (not in Docker)           │   containerC       →  containerC (YAML-only; ID = defaultID("containerC"))
```

---

## Key Code Snippets

### `targetInfoEqual` (proxy/server.go)

```go
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
        a.HTTPHealthCheck == b.HTTPHealthCheck
}
```

### Empty placeholder written by `Store.Load()` when file is missing

```yaml
services: []
```

### Atomic file write in `Store.Save()`

```go
tmp := s.path + ".tmp"
if err := os.WriteFile(tmp, data, 0o644); err != nil { return err }
return os.Rename(tmp, s.path)
```

---

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestStoreLoad_MissingFile` | `CONFIG_PATH` points to non-existent file | File created with `services: []`; `Load()` returns nil |
| `TestStoreLoad_ValidYAML` | YAML file with one service entry | `store.Get().Services` has one entry |
| `TestStoreLoad_InvalidYAML` | Malformed YAML | `Load()` returns non-nil error |
| `TestApply_Override` | One discovered target + matching YAML entry | YAML fields replace label fields; ContainerID preserved |
| `TestApply_YAMLOnly` | No discovered targets + one YAML entry | Entry added with `defaultID(name)` as ContainerID |
| `TestApply_NoYAML` | Two discovered targets + empty config | Result equals discovered unchanged |
| `TestApply_InvalidEntry` | YAML entry with no ports | Entry skipped; error returned; other valid entries applied |
| `TestAdminAuth_Missing` | Request with no `X-API-Key` | `401` |
| `TestAdminAuth_Wrong` | Request with wrong key | `401` |
| `TestAdminGetConfig` | Valid key + loaded store | `200` with JSON matching `DynamicConfig` |
| `TestAdminReload` | Valid key | `200 {"status":"ok","services_loaded":N}` |
| `TestAdminUpdate_Valid` | Valid JSON body | `200`; YAML file updated on disk |
| `TestAdminUpdate_InvalidJSON` | Malformed JSON | `400`; YAML file NOT modified |
| `TestProxyUpdate_AddRemove` | New targets list missing one current target | Old target removed; new target registered |
| `TestProxyUpdate_NoChange` | Same targets list | No listeners recreated |

---

## Risks & Open Questions

1. **WatchEvents bypass**: Newly started containers discovered via `WatchEvents` call
   `srv.RegisterTarget` directly — they bypass the YAML overlay. This is acceptable for now
   because `WatchEvents` only fires for labeled containers. Users can call `GET /config/reload`
   to re-apply the overlay for YAML-only containers that were recreated.

2. **YAML-only containers not auto-discovered on start**: If a YAML-only container starts after
   the proxy, the proxy won't pick it up until `GET /config/reload`. A future improvement could
   watch Docker/K8s events for all containers and check the config store.

3. **Port conflicts during `Update()`**: If a target's listen port changes, `RemoveTarget` then
   `RegisterTarget` creates a brief window (milliseconds) where the port is unbound. Connections
   arriving in that window get "connection refused". This is acceptable for a config reload path.

4. **`reflect.DeepEqual` on `[]net.IPNet`**: `net.IPNet` contains a `net.IP` (byte slice) and
   `net.IPMask` (byte slice). `reflect.DeepEqual` handles these correctly.
