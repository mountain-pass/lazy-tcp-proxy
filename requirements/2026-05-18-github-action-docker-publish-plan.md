# GitHub Actions Docker Hub Publish Workflow — Implementation Plan

**Requirement**: [2026-05-18-github-action-docker-publish.md](2026-05-18-github-action-docker-publish.md)
**Date**: 2026-05-18
**Status**: Draft

## Implementation Steps

1. Create `.github/workflows/docker-publish.yml` with the full workflow definition (see Key Code Snippets below).
2. Update requirement status to "Completed" in `requirements/2026-05-18-github-action-docker-publish.md` and `requirements/_index.md`.
3. Commit and push all changes to `claude/github-action-docker-publish-wJQpG`.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `.github/workflows/docker-publish.yml` | Create | New workflow: build + push Docker images, create GitHub release |
| `requirements/2026-05-18-github-action-docker-publish.md` | Modify | Status → Completed |
| `requirements/_index.md` | Modify | Status → Completed |

## Key Code Snippets

### `.github/workflows/docker-publish.yml`

```yaml
name: Docker Publish

on:
  push:
    branches: ["main"]

permissions:
  contents: write   # required for gh release create

jobs:
  docker-publish:
    name: Build & Publish Docker Image
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Compute version
        id: version
        run: |
          VERSION="1.$(date +%Y%m%d).$(git rev-parse --short=8 HEAD)"
          echo "version=$VERSION" >> "$GITHUB_OUTPUT"

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: ./lazy-tcp-proxy
          platforms: linux/amd64,linux/arm64,linux/arm/v7
          push: true
          tags: |
            mountainpass/lazy-tcp-proxy:docker-${{ steps.version.outputs.version }}
            mountainpass/lazy-tcp-proxy:latest

      - name: Create GitHub release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          VERSION="${{ steps.version.outputs.version }}"
          DOCKERHUB_URL="https://hub.docker.com/repository/docker/mountainpass/lazy-tcp-proxy/tags/docker-${VERSION}/"
          gh release create "${VERSION}" \
            --title "${VERSION}" \
            --notes "Docker image: ${DOCKERHUB_URL}"
```

**Notes on key decisions:**

- `actions/checkout@v4` — v4 is the current stable release for the checkout action (the existing CI uses v6 which appears to be a pre-release; staying consistent would also be fine but v4 is the widely-deployed stable version).
- No `fetch-depth: 0` needed — `git rev-parse --short=8 HEAD` only needs the current commit, which a shallow clone provides.
- `docker/build-push-action@v6` — current major; handles multi-platform export via BuildKit automatically.
- `docker/setup-buildx-action@v3` — creates a `docker-container` builder that supports `--platform` with multiple targets. No QEMU action needed because the Dockerfile compiles on the host arch via Go's native cross-compilation.
- `GITHUB_TOKEN` is used for `gh release create` — no extra secret required; just the `contents: write` permission.
- The Docker Hub URL in the release notes uses the `docker-$VERSION` tag name (not bare `$VERSION`) since that is the actual tag pushed.

## Risks & Open Questions

- **Secret configuration**: The workflow will silently succeed in building but fail at the push step if `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` are not set. These must be configured in the repository's Settings → Secrets → Actions before the first push to `main`.
- **Release name collision**: If the same short SHA is re-used (e.g., force-pushed), `gh release create` will fail with "release already exists". This is an acceptable guard against accidental re-publication.
- **k8s variant**: Not covered here; can be added as a second build step in a follow-up requirement.
