package portainer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvVars_withDefaults(t *testing.T) {
	result := parseEnvVars("image: ${IMG:-nginx:latest}")
	if len(result) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(result))
	}
	ev := result[0]
	if ev.Name != "IMG" || ev.Label != "IMG" || ev.Default == nil || *ev.Default != "nginx:latest" {
		t.Errorf("unexpected env var: %+v", ev)
	}
}

func TestParseEnvVars_noDefault(t *testing.T) {
	result := parseEnvVars("user: ${USER}")
	if len(result) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(result))
	}
	ev := result[0]
	if ev.Name != "USER" || ev.Default != nil {
		t.Errorf("unexpected env var: %+v", ev)
	}
}

func TestParseEnvVars_deduplication(t *testing.T) {
	result := parseEnvVars("${X:-a} ${X:-b}")
	if len(result) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(result))
	}
	if result[0].Default == nil || *result[0].Default != "a" {
		t.Errorf("expected first occurrence default 'a', got %v", result[0].Default)
	}
}

func TestParseEnvVars_mixed(t *testing.T) {
	content := `
POSTGRES_USER: ${POSTGRES_USER:-admin}
POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-password}
POSTGRES_DB: ${POSTGRES_DB:-postgres}
`
	result := parseEnvVars(content)
	if len(result) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(result))
	}
	expected := []struct {
		name string
		def  string
	}{
		{"POSTGRES_USER", "admin"},
		{"POSTGRES_PASSWORD", "password"},
		{"POSTGRES_DB", "postgres"},
	}
	for i, e := range expected {
		if result[i].Name != e.name {
			t.Errorf("[%d] name: want %q got %q", i, e.name, result[i].Name)
		}
		if result[i].Default == nil || *result[i].Default != e.def {
			t.Errorf("[%d] default: want %q got %v", i, e.def, result[i].Default)
		}
	}
}

func TestBuildTemplates_emptyDir(t *testing.T) {
	out := BuildTemplates("/nonexistent/path/that/does/not/exist")
	if out.Version != "2" {
		t.Errorf("expected version '2', got %q", out.Version)
	}
	if len(out.Templates) != 0 {
		t.Errorf("expected 0 templates, got %d", len(out.Templates))
	}
}

func TestBuildTemplates_singleRecipe(t *testing.T) {
	dir := t.TempDir()
	content := "name: whoami\nservices:\n  whoami:\n    image: traefik/whoami\n    environment:\n      PORT: ${PORT:-80}\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.whoami.9003.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := BuildTemplates(dir)
	if len(out.Templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(out.Templates))
	}
	tmpl := out.Templates[0]
	if tmpl.Title != "docker-compose.whoami.9003" {
		t.Errorf("title: want %q got %q", "docker-compose.whoami.9003", tmpl.Title)
	}
	if tmpl.Type != 3 {
		t.Errorf("type: want 3 got %d", tmpl.Type)
	}
	if tmpl.Compose != content {
		t.Errorf("compose mismatch")
	}
	if len(tmpl.Env) != 1 || tmpl.Env[0].Name != "PORT" {
		t.Errorf("unexpected env: %+v", tmpl.Env)
	}
}

func TestBuildTemplates_sorted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"docker-compose.z.yml", "docker-compose.a.yml", "docker-compose.m.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("name: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := BuildTemplates(dir)
	if len(out.Templates) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(out.Templates))
	}
	titles := []string{out.Templates[0].Title, out.Templates[1].Title, out.Templates[2].Title}
	want := []string{"docker-compose.a", "docker-compose.m", "docker-compose.z"}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("[%d] title: want %q got %q", i, want[i], titles[i])
		}
	}
}

func TestHandler_returnsJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.test.yml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/portainer", nil)
	w := httptest.NewRecorder()
	Handler(dir)(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: want 200 got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: want application/json got %q", ct)
	}
	var out AppTemplates
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Version != "2" {
		t.Errorf("version: want '2' got %q", out.Version)
	}
	if len(out.Templates) != 1 {
		t.Errorf("expected 1 template, got %d", len(out.Templates))
	}
}
