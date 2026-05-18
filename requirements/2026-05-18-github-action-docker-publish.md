# GitHub Actions Docker Hub Publish Workflow

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Completed

## Problem Statement

There is no automated mechanism to build and publish Docker images to Docker Hub when commits land on `main`. Releases must be created manually, which is slow and error-prone.

## Functional Requirements

1. On every push to `main`, a GitHub Actions workflow must:
   - Build multi-platform Docker images for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.
   - Push two tagged images to Docker Hub:
     - `mountainpass/lazy-tcp-proxy:docker-<VERSION>`
     - `mountainpass/lazy-tcp-proxy:latest`
   - Create a GitHub release named `<VERSION>` whose body links to the Docker Hub tags page.

2. `VERSION` is computed as `1.<YYYYMMDD>.<8-char-git-sha>` (matching the pattern the team already uses manually).

3. The workflow must **not** run on pull requests — only on push to `main`.

4. Docker Hub credentials (`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`) are stored as GitHub Actions secrets.

## User Experience Requirements

- No manual steps after merging to `main`; the image and release appear automatically.
- The GitHub release notes contain the full Docker Hub URL so users can find the image immediately.

## Technical Requirements

- Use `docker/setup-buildx-action` (no QEMU needed — the Dockerfile already uses native Go cross-compilation via `BUILDPLATFORM` / `TARGETARCH`).
- Use `docker/login-action` for Docker Hub authentication.
- Use `docker/build-push-action` for the multi-platform build and push.
- Use `gh release create` (available on GitHub-hosted runners) to create the release; requires `contents: write` permission.
- The `DIGITALOCEAN_PVT_KEY` secret is not needed for this workflow — native Go cross-compilation in the Dockerfile eliminates the need for a remote ARM builder.

## Acceptance Criteria

- [ ] Pushing to `main` triggers the workflow automatically.
- [ ] Both `mountainpass/lazy-tcp-proxy:docker-<VERSION>` and `mountainpass/lazy-tcp-proxy:latest` appear on Docker Hub after the workflow completes.
- [ ] Images are present for all three target platforms (`linux/amd64`, `linux/arm64`, `linux/arm/v7`).
- [ ] A GitHub release named `<VERSION>` is created with a body containing the Docker Hub URL.
- [ ] The workflow does not run on pull requests.
- [ ] Secrets (`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`) are never exposed in logs.

## Dependencies

- Requires `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` to be configured as GitHub Actions secrets in the repository settings.
- Builds on REQ-031 (GitHub Actions Go CI) — the new workflow is a separate file and does not modify the existing CI workflow.
- Relies on REQ-047 (native cross-platform Docker build) — the existing Dockerfile already supports `linux/amd64`, `linux/arm64`, and `linux/arm/v7` without QEMU.

## Implementation Notes

- Workflow file: `.github/workflows/docker-publish.yml`
- The `gh` CLI is pre-installed on `ubuntu-latest` GitHub-hosted runners.
- A future enhancement could extend this to also publish `mountainpass/lazy-tcp-proxy-k8s` (the Kubernetes build artifact from REQ-049), but that is out of scope for this requirement.
