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
	Type       int        `json:"type"`
	Title      string     `json:"title"`
	Repository Repository `json:"repository"`
	Env        []EnvVar   `json:"env"`
}

// Repository points Portainer at the git endpoint and stackfile path.
type Repository struct {
	URL       string `json:"url"`
	StackFile string `json:"stackfile"`
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
// App Templates v2 payload. baseURL is the scheme+host used to build the
// repository.url field (e.g. "http://192.168.1.1:8080"). Returns an empty
// templates list if the directory does not exist or contains no yml files.
func BuildTemplates(recipesDir, baseURL string) AppTemplates {
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
		filename := filepath.Base(path)
		title := strings.TrimSuffix(filename, ".yml")
		out.Templates = append(out.Templates, Template{
			Type:  3,
			Title: title,
			Repository: Repository{
				URL:       baseURL + "/portainer/git",
				StackFile: filename,
			},
			Env: parseEnvVars(string(data)),
		})
	}
	return out
}

// TemplatesHandler returns an http.HandlerFunc that serves the Portainer App
// Templates v2 JSON. The repository URL is derived from the incoming request's
// Host header so it is correct behind reverse proxies.
func TemplatesHandler(recipesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL := scheme + "://" + r.Host
		tmpl := BuildTemplates(recipesDir, baseURL)
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(tmpl) //nolint:errcheck
	}
}
