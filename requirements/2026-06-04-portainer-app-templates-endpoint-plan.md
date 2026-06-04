# Portainer App Templates Endpoint — Implementation Plan

**Requirement**: [2026-06-04-portainer-app-templates-endpoint.md](2026-06-04-portainer-app-templates-endpoint.md)
**Date**: 2026-06-04
**Status**: Implemented

## Implementation Steps

1. **Create `lazy-tcp-proxy/internal/portainer/templates.go`** — new package containing:
   - Types: `AppTemplates`, `Template`, `EnvVar`
   - `resolveRecipesDir()` — reads `RECIPES_DIR` env var, falls back to `"./recipes"`
   - `parseEnvVars(content string) []EnvVar` — scans raw text with regex `\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}`, deduplicates by name, returns sorted slice
   - `BuildTemplates(recipesDir string) (AppTemplates, error)` — globs `*.yml` files from the dir, reads each, builds a `Template` per file
   - `Handler(recipesDir string) http.HandlerFunc` — returns a handler that calls `BuildTemplates` and writes JSON

2. **Wire the handler into `main.go`** — in `runStatusServer`, add:
   ```go
   recipesDir := resolveRecipesDir()
   mux.HandleFunc("/portainer", portainerpkg.Handler(recipesDir))
   ```
   Add import `portainerpkg "github.com/mountain-pass/lazy-tcp-proxy/internal/portainer"`.
   Add `resolveRecipesDir()` function alongside the other `resolve*` functions.
   Log the portainer endpoint at startup.

3. **Create `lazy-tcp-proxy/internal/portainer/templates_test.go`** — unit tests for `parseEnvVars` and `BuildTemplates`.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/portainer/templates.go` | Create | Core logic: types, env var parser, template builder, HTTP handler |
| `lazy-tcp-proxy/internal/portainer/templates_test.go` | Create | Unit tests for parser and builder |
| `lazy-tcp-proxy/main.go` | Modify | Add `resolveRecipesDir()`, wire `/portainer` handler, add startup log |

## API Contracts

### `GET /portainer`

**Response** `200 OK`, `Content-Type: application/json`

```json
{
  "version": "2",
  "templates": [
    {
      "type": 3,
      "title": "docker-compose.postgres.5432",
      "description": "",
      "logo": "",
      "compose": "name: postgres\n\nservices:\n  postgres:\n...",
      "env": [
        { "name": "POSTGRES_USER",     "label": "POSTGRES_USER",     "default": "admin" },
        { "name": "POSTGRES_PASSWORD", "label": "POSTGRES_PASSWORD", "default": "password" },
        { "name": "POSTGRES_DB",       "label": "POSTGRES_DB",       "default": "postgres" }
      ]
    }
  ]
}
```

Empty state (no recipes dir or empty dir):
```json
{ "version": "2", "templates": [] }
```

## Data Models

```go
// AppTemplates is the top-level Portainer App Templates v2 response.
type AppTemplates struct {
    Version   string     `json:"version"`
    Templates []Template `json:"templates"`
}

// Template is a single Portainer App Template entry (type 3 = Docker Compose stack).
type Template struct {
    Type        int      `json:"type"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    Logo        string   `json:"logo"`
    Compose     string   `json:"compose"`
    Env         []EnvVar `json:"env"`
}

// EnvVar is a configurable environment variable for a Portainer template.
type EnvVar struct {
    Name    string  `json:"name"`
    Label   string  `json:"label"`
    Default *string `json:"default,omitempty"`
}
```

## Key Code Snippets

**Env var regex** (compile once as a package-level var):
```go
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}`)
```

**`parseEnvVars`** — scan, deduplicate, return in first-seen order:
```go
func parseEnvVars(content string) []EnvVar {
    seen := make(map[string]bool)
    var result []EnvVar
    for _, m := range envVarRe.FindAllStringSubmatch(content, -1) {
        name := m[1]
        if seen[name] { continue }
        seen[name] = true
        ev := EnvVar{Name: name, Label: name}
        if m[2] != "" {
            d := m[2]
            ev.Default = &d
        }
        result = append(result, ev)
    }
    return result
}
```

**`BuildTemplates`**:
```go
func BuildTemplates(recipesDir string) (AppTemplates, error) {
    out := AppTemplates{Version: "2", Templates: []Template{}}
    entries, err := filepath.Glob(filepath.Join(recipesDir, "*.yml"))
    if err != nil || len(entries) == 0 {
        return out, nil
    }
    sort.Strings(entries)
    for _, path := range entries {
        data, err := os.ReadFile(path)
        if err != nil { continue }
        content := string(data)
        title := strings.TrimSuffix(filepath.Base(path), ".yml")
        out.Templates = append(out.Templates, Template{
            Type:        3,
            Title:       title,
            Description: "",
            Logo:        "",
            Compose:     content,
            Env:         parseEnvVars(content),
        })
    }
    return out, nil
}
```

**`Handler`**:
```go
func Handler(recipesDir string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        tmpl, _ := BuildTemplates(recipesDir)
        w.Header().Set("Content-Type", "application/json")
        enc := json.NewEncoder(w)
        enc.SetIndent("", "  ")
        enc.Encode(tmpl) //nolint:errcheck
    }
}
```

**`resolveRecipesDir`** in `main.go`:
```go
func resolveRecipesDir() string {
    if v := os.Getenv("RECIPES_DIR"); v != "" {
        return v
    }
    return "./recipes"
}
```

**Wiring in `runStatusServer`** — add as a parameter and register:
```go
func runStatusServer(ctx context.Context, ..., recipesDir string) {
    ...
    mux.HandleFunc("/portainer", portainerpkg.Handler(recipesDir))
    ...
}
```
And in `main()`:
```go
recipesDir := resolveRecipesDir()
log.Printf("portainer app templates: GET /portainer available (RECIPES_DIR=%s)", recipesDir)
runStatusServer(ctx, srv, webPort, ..., recipesDir)
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestParseEnvVars_withDefaults` | `"image: ${IMG:-nginx:latest}"` | `[{Name:"IMG", Label:"IMG", Default:ptr("nginx:latest")}]` |
| `TestParseEnvVars_noDefault` | `"user: ${USER}"` | `[{Name:"USER", Label:"USER", Default:nil}]` |
| `TestParseEnvVars_deduplication` | `"${X:-a} ${X:-b}"` | single entry for `X` with default `"a"` (first occurrence wins) |
| `TestParseEnvVars_mixed` | postgres recipe content | all three POSTGRES_* vars detected with correct defaults |
| `TestBuildTemplates_emptyDir` | non-existent dir | `AppTemplates{Version:"2", Templates:[]}` |
| `TestBuildTemplates_singleRecipe` | dir with one file `docker-compose.whoami.9003.yml` | one template, `title="docker-compose.whoami.9003"`, compose = file content, env detected |
| `TestBuildTemplates_sorted` | dir with multiple files | templates in alphabetical filename order |
| `TestHandler_returnsJSON` | HTTP GET `/portainer` against temp dir | 200, `Content-Type: application/json`, valid JSON |

## Risks & Open Questions

- **Recipes dir in container**: the default `./recipes` is relative to the working directory of the process. In the Docker image the working directory is `/` so `./recipes` won't exist unless the image is rebuilt with the recipes bundled or `RECIPES_DIR` is set. This is acceptable for MVP — users set `RECIPES_DIR` to mount recipes in.
- **`EnvVar.Default` as pointer**: using `*string` lets `omitempty` suppress the field when no default is present. An empty string default (e.g. `${VAR:-}`) will produce `"default": ""` which is correct Portainer behaviour.
- **`runStatusServer` signature change**: adding `recipesDir string` as a new last parameter is a non-breaking internal change (only called once from `main()`).
