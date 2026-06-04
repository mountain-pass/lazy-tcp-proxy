package portainer

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

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

var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}`)

// parseEnvVars scans raw YAML text for ${VAR} and ${VAR:-default} substitutions,
// deduplicates by name (first occurrence wins), and returns them in order.
func parseEnvVars(content string) []EnvVar {
	seen := make(map[string]bool)
	var result []EnvVar
	for _, m := range envVarRe.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
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

// BuildTemplates reads all *.yml files from recipesDir and returns a Portainer
// App Templates v2 payload. Returns an empty templates list if the directory
// does not exist or contains no yml files.
func BuildTemplates(recipesDir string) AppTemplates {
	out := AppTemplates{Version: "2", Templates: []Template{}}
	entries, err := filepath.Glob(filepath.Join(recipesDir, "*.yml"))
	if err != nil || len(entries) == 0 {
		return out
	}
	sort.Strings(entries)
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
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
	return out
}

// Handler returns an http.HandlerFunc that serves the Portainer App Templates
// v2 JSON for all recipe files in recipesDir.
func Handler(recipesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl := BuildTemplates(recipesDir)
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(tmpl) //nolint:errcheck
	}
}
