# Portainer Template Metadata Override via `x-portainer` YAML Section — Implementation Plan

**Requirement**: [2026-06-04-portainer-x-portainer-metadata-override.md](2026-06-04-portainer-x-portainer-metadata-override.md)
**Date**: 2026-06-04
**Status**: Implemented

## Implementation Steps

1. **Add new fields to `Template` struct** in `lazy-tcp-proxy/internal/portainer/templates.go`:
   - `Description string` → `json:"description,omitempty"`
   - `AdministratorOnly bool` → `json:"administrator_only,omitempty"`
   - `Logo string` → `json:"logo,omitempty"`
   - `Categories []string` → `json:"categories,omitempty"`
   - `Note string` → `json:"note,omitempty"`

2. **Add `xPortainerMeta` struct** (unexported) to represent the parsed `x-portainer` block:
   ```go
   type xPortainerMeta struct {
       Title             string   `yaml:"title"`
       Description       string   `yaml:"description"`
       AdministratorOnly bool     `yaml:"administrator-only"`
       Logo              string   `yaml:"logo"`
       Categories        []string `yaml:"categories"`
       Note              string   `yaml:"note"`
   }
   ```

3. **Add `parseXPortainer(content string) xPortainerMeta` function**:
   - Unmarshal only the `x-portainer` key using `gopkg.in/yaml.v3` into a `map[string]interface{}` wrapper, then re-marshal and unmarshal into `xPortainerMeta`.
   - Simpler alternative: unmarshal the entire document into a `struct { XPortainer xPortainerMeta \`yaml:"x-portainer"\` }` — use this approach.
   - On any unmarshal error, return a zero-value `xPortainerMeta` (logged at no level — parse errors are silent but the template still renders).

4. **Extend `BuildTemplates` loop** — after reading file content and constructing the default `Template`, call `parseXPortainer` and apply overrides:
   - If `meta.Title != ""` → override `tmpl.Title`
   - Always copy `meta.Description`, `meta.AdministratorOnly`, `meta.Logo`, `meta.Note` (zero values will be omitted by `omitempty`)
   - If `meta.Categories != nil` → set `tmpl.Categories`

5. **Add import** `"gopkg.in/yaml.v3"` to `templates.go`.

6. **Add unit tests** to `templates_test.go`:
   - `TestParseXPortainer_allFields` — YAML with all six fields, verify struct values.
   - `TestParseXPortainer_partial` — only `title` and `logo` set, others zero.
   - `TestParseXPortainer_absent` — YAML with no `x-portainer` key, verify zero struct returned.
   - `TestParseXPortainer_malformed` — invalid YAML, verify zero struct returned (no panic).
   - `TestBuildTemplates_xPortainerOverride` — write a temp recipe file with an `x-portainer` block, call `BuildTemplates`, assert overridden `Title`, `Description`, `Logo`, `Categories`, `Note` appear in the output template.
   - `TestBuildTemplates_noXPortainer` — existing recipe without `x-portainer` still produces correct default title (regression guard).

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/portainer/templates.go` | Modify | Add fields to `Template`, add `xPortainerMeta` struct, add `parseXPortainer`, update `BuildTemplates` loop, add `gopkg.in/yaml.v3` import |
| `lazy-tcp-proxy/internal/portainer/templates_test.go` | Modify | Add 6 new test cases covering override, partial, absent, malformed, and regression scenarios |

## API Contracts

No HTTP API changes. New JSON fields appear in the `/portainer/templates` response only when set:

```json
{
  "type": 3,
  "title": "S3 Bucket",
  "description": "A simple s3 bucket - one user, one bucket.",
  "administrator_only": false,
  "logo": "data:image/png;base64,iVBORw0KGgo...",
  "categories": ["storage"],
  "note": "Not for the faint of heart.",
  "repository": { ... },
  "env": [ ... ]
}
```

## Key Code Snippets

```go
// xPortainerMeta holds the optional x-portainer override block from a recipe file.
type xPortainerMeta struct {
    Title             string   `yaml:"title"`
    Description       string   `yaml:"description"`
    AdministratorOnly bool     `yaml:"administrator-only"`
    Logo              string   `yaml:"logo"`
    Categories        []string `yaml:"categories"`
    Note              string   `yaml:"note"`
}

func parseXPortainer(content string) xPortainerMeta {
    var doc struct {
        XPortainer xPortainerMeta `yaml:"x-portainer"`
    }
    _ = yaml.Unmarshal([]byte(content), &doc)
    return doc.XPortainer
}
```

In `BuildTemplates` loop, after building the default template:
```go
meta := parseXPortainer(string(data))
if meta.Title != "" {
    tmpl.Title = meta.Title
}
tmpl.Description = meta.Description
tmpl.AdministratorOnly = meta.AdministratorOnly
tmpl.Logo = meta.Logo
tmpl.Categories = meta.Categories
tmpl.Note = meta.Note
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestParseXPortainer_allFields` | YAML with all six `x-portainer` fields | Struct with all fields populated |
| `TestParseXPortainer_partial` | YAML with only `title` and `logo` | Only those two fields non-zero |
| `TestParseXPortainer_absent` | YAML with no `x-portainer` key | Zero-value struct |
| `TestParseXPortainer_malformed` | `"x-portainer: !!invalid"` | Zero-value struct, no panic |
| `TestBuildTemplates_xPortainerOverride` | Recipe file with full `x-portainer` block | Template fields match overrides |
| `TestBuildTemplates_noXPortainer` | Recipe file without `x-portainer` | Title still derived from filename (regression) |

## Risks & Open Questions

- `administrator_only: false` is the zero value for bool — with `omitempty`, a false value will be omitted from JSON. Portainer's default when the field is absent is also `false`, so this is fine. If someone explicitly sets `administrator-only: true` in the YAML it will appear in JSON correctly.
- `gopkg.in/yaml.v3` is a transitive dependency (confirmed via `go list -m`); it is not directly imported in the `portainer` package today. Adding a direct import is safe — it won't change `go.sum`.
