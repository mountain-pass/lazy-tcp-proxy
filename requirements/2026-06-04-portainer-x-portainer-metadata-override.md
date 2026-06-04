# Portainer Template Metadata Override via `x-portainer` YAML Section

**Date Added**: 2026-06-04
**Priority**: Medium
**Status**: Completed

## Problem Statement

The `/portainer/templates` endpoint derives template metadata (title, description, logo, etc.) purely from the recipe filename and file content. There is no way for recipe authors to enrich or override fields like a human-friendly title, a description, a logo, categories, an administrator-only flag, or a note without modifying the Go source.

## Functional Requirements

1. Each recipe YAML file may include an optional top-level `x-portainer` section.
2. All fields within `x-portainer` are optional. When a field is present, it replaces the corresponding field in the generated Portainer template entry. When absent, the existing default behaviour is preserved.
3. Supported fields and their mapping to Portainer template fields:

   | `x-portainer` key      | Portainer template field | Type             |
   |------------------------|--------------------------|------------------|
   | `title`                | `title`                  | string           |
   | `description`          | `description`            | string           |
   | `administrator-only`   | `administrator_only`     | bool             |
   | `logo`                 | `logo`                   | string (URL or data URI) |
   | `categories`           | `categories`             | array of strings |
   | `note`                 | `note`                   | string           |

4. Parsing is done on the raw YAML text (no external YAML library) using `gopkg.in/yaml.v3` (already a transitive dependency) or stdlib-compatible parsing. If a YAML library is not available, a targeted `gopkg.in/yaml.v3` unmarshal of only the `x-portainer` key is acceptable.
5. If the `x-portainer` section is malformed, the recipe is still included with its default metadata; the parsing error is logged at debug level only.

## User Experience Requirements

- Recipe authors add an `x-portainer` block near the top of their YAML file to enrich the Portainer UI display.
- No changes are required in the lazy-tcp-proxy configuration or environment variables.

## Technical Requirements

- Implementation extends `BuildTemplates` / the recipe-loading loop in `lazy-tcp-proxy/internal/portainer/templates.go`.
- The `Template` struct gains the additional fields: `Description`, `AdministratorOnly`, `Logo`, `Categories`, `Note` — marshalled with `omitempty` so absent fields produce no JSON keys.
- Parsing of `x-portainer` must not introduce new external module dependencies unless `gopkg.in/yaml.v3` is already present in `go.mod`.

## Acceptance Criteria

- [ ] A recipe file containing an `x-portainer` section has its fields reflected in the `/portainer/templates` JSON response.
- [ ] `title` in `x-portainer` overrides the filename-derived title.
- [ ] `description`, `administrator-only`, `logo`, `categories`, and `note` are present in the JSON when set in `x-portainer`.
- [ ] A recipe file with no `x-portainer` section behaves identically to the current implementation (no regression).
- [ ] A recipe file with a malformed `x-portainer` section is included in the response with default metadata; no HTTP 500 is returned.
- [ ] Unit tests cover: field override, partial override, no section present, malformed section.
- [ ] `go test ./...` passes.
- [ ] `golangci-lint run` passes with no new violations.

## Dependencies

- REQ-097 (Portainer App Templates Endpoint) — the endpoint being extended
- REQ-099 (Portainer Git HTTP Endpoint) — parallel portainer work on the same branch

## Implementation Notes

- `x-portainer` is a YAML extension key (prefixed with `x-`) and is ignored by Docker Compose tooling, making it safe to add to any recipe file.
- `gopkg.in/yaml.v3` is already present in the module graph via compose/buildkit dependencies; confirm with `go list -m gopkg.in/yaml.v3` before using it.
