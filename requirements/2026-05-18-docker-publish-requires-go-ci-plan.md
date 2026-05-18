# Gate Docker Publish on Go CI Success — Implementation Plan

**Requirement**: [2026-05-18-docker-publish-requires-go-ci.md](2026-05-18-docker-publish-requires-go-ci.md)
**Date**: 2026-05-18
**Status**: Draft

## Implementation Steps

1. Edit `.github/workflows/docker-publish.yml`: replace the `on.push` trigger with `on.workflow_run` and add a job-level `if` guard.
2. Update requirement status to "Completed" in `requirements/2026-05-18-docker-publish-requires-go-ci.md` and `requirements/_index.md`.
3. Commit and push.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `.github/workflows/docker-publish.yml` | Modify | Replace trigger; add job `if` guard |
| `requirements/2026-05-18-docker-publish-requires-go-ci.md` | Modify | Status → Completed |
| `requirements/_index.md` | Modify | Status → Completed |

## Key Code Snippets

Replace:

```yaml
on:
  push:
    branches: ["main"]
```

With:

```yaml
on:
  workflow_run:
    workflows: ["Go CI"]
    branches: ["main"]
    types: [completed]
```

Add `if` on the job (the string `"Go CI"` must match the `name:` field in `go-ci.yml` exactly):

```yaml
jobs:
  docker-publish:
    name: Build & Publish Docker Image
    runs-on: ubuntu-latest
    if: ${{ github.event.workflow_run.conclusion == 'success' }}
```

## Risks & Open Questions

- The `workflows: ["Go CI"]` string is matched against the `name:` field in the upstream workflow file, not the filename. The current value in `go-ci.yml` is `name: Go CI` — these must stay in sync if the CI workflow is ever renamed.
