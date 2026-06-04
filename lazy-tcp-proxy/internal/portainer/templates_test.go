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
	out := BuildTemplates("/nonexistent/path/that/does/not/exist", "http://localhost")
	if out.Version != "3" {
		t.Errorf("expected version '3', got %q", out.Version)
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

	out := BuildTemplates(dir, "http://localhost:8080")
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
	if tmpl.Repository.URL != "http://localhost:8080/portainer/git" {
		t.Errorf("repository.url: want %q got %q", "http://localhost:8080/portainer/git", tmpl.Repository.URL)
	}
	if tmpl.Repository.StackFile != "docker-compose.whoami.9003.yml" {
		t.Errorf("repository.stackfile: want %q got %q", "docker-compose.whoami.9003.yml", tmpl.Repository.StackFile)
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
	out := BuildTemplates(dir, "http://localhost")
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

func TestParseXPortainer_allFields(t *testing.T) {
	content := `
x-portainer:
  title: S3 Bucket
  description: A simple s3 bucket - one user, one bucket.
  administrator-only: true
  logo: data:image/png;base64,abc123
  categories:
    - storage
    - aws
  note: Not for the faint of heart.
`
	meta := parseXPortainer(content)
	if meta.Title != "S3 Bucket" {
		t.Errorf("title: want %q got %q", "S3 Bucket", meta.Title)
	}
	if meta.Description != "A simple s3 bucket - one user, one bucket." {
		t.Errorf("description mismatch: %q", meta.Description)
	}
	if !meta.AdministratorOnly {
		t.Error("administrator-only: want true got false")
	}
	if meta.Logo != "data:image/png;base64,abc123" {
		t.Errorf("logo mismatch: %q", meta.Logo)
	}
	if len(meta.Categories) != 2 || meta.Categories[0] != "storage" || meta.Categories[1] != "aws" {
		t.Errorf("categories mismatch: %v", meta.Categories)
	}
	if meta.Note != "Not for the faint of heart." {
		t.Errorf("note mismatch: %q", meta.Note)
	}
}

func TestParseXPortainer_partial(t *testing.T) {
	content := `
x-portainer:
  title: My Service
  logo: https://example.com/logo.png
`
	meta := parseXPortainer(content)
	if meta.Title != "My Service" {
		t.Errorf("title: want %q got %q", "My Service", meta.Title)
	}
	if meta.Logo != "https://example.com/logo.png" {
		t.Errorf("logo mismatch: %q", meta.Logo)
	}
	if meta.Description != "" || meta.Note != "" || meta.Categories != nil || meta.AdministratorOnly {
		t.Errorf("unexpected non-zero fields in partial parse: %+v", meta)
	}
}

func TestParseXPortainer_absent(t *testing.T) {
	content := "name: myservice\nservices:\n  web:\n    image: nginx\n"
	meta := parseXPortainer(content)
	if meta.Title != "" || meta.Description != "" || meta.Logo != "" || meta.Note != "" || meta.Categories != nil || meta.AdministratorOnly {
		t.Errorf("expected zero struct for absent x-portainer, got: %+v", meta)
	}
}

func TestParseXPortainer_malformed(t *testing.T) {
	content := "x-portainer: !!binary invalid\n"
	meta := parseXPortainer(content)
	// Should not panic; zero struct returned
	_ = meta
}

func TestBuildTemplates_xPortainerOverride(t *testing.T) {
	dir := t.TempDir()
	content := `name: minio
x-portainer:
  title: S3 Bucket
  description: A simple s3 bucket.
  logo: data:image/png;base64,abc
  categories:
    - storage
  note: Handle with care.
services:
  minio:
    image: minio/minio
    environment:
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:-secret}
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.minio.9000.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := BuildTemplates(dir, "http://localhost:8080")
	if len(out.Templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(out.Templates))
	}
	tmpl := out.Templates[0]
	if tmpl.Title != "S3 Bucket" {
		t.Errorf("title: want %q got %q", "S3 Bucket", tmpl.Title)
	}
	if tmpl.Description != "A simple s3 bucket." {
		t.Errorf("description: want %q got %q", "A simple s3 bucket.", tmpl.Description)
	}
	if tmpl.Logo != "data:image/png;base64,abc" {
		t.Errorf("logo mismatch: %q", tmpl.Logo)
	}
	if len(tmpl.Categories) != 1 || tmpl.Categories[0] != "storage" {
		t.Errorf("categories mismatch: %v", tmpl.Categories)
	}
	if tmpl.Note != "Handle with care." {
		t.Errorf("note mismatch: %q", tmpl.Note)
	}
	// Env detection still works alongside x-portainer
	if len(tmpl.Env) != 1 || tmpl.Env[0].Name != "MINIO_ROOT_PASSWORD" {
		t.Errorf("env mismatch: %v", tmpl.Env)
	}
}

func TestBuildTemplates_noXPortainer_regression(t *testing.T) {
	dir := t.TempDir()
	content := "name: redis\nservices:\n  redis:\n    image: redis\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.redis.6379.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := BuildTemplates(dir, "http://localhost:8080")
	if len(out.Templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(out.Templates))
	}
	if out.Templates[0].Title != "docker-compose.redis.6379" {
		t.Errorf("title regression: want filename-derived title, got %q", out.Templates[0].Title)
	}
}

func TestTemplatesHandler_returnsJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.test.yml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/portainer/templates", nil)
	w := httptest.NewRecorder()
	TemplatesHandler(dir, "http://localhost:8080")(w, req)

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
	if out.Version != "3" {
		t.Errorf("version: want '3' got %q", out.Version)
	}
	if len(out.Templates) != 1 {
		t.Errorf("expected 1 template, got %d", len(out.Templates))
	}
	if out.Templates[0].Repository.URL != "http://localhost:8080/portainer/git" {
		t.Errorf("repository.url mismatch: %q", out.Templates[0].Repository.URL)
	}
}
