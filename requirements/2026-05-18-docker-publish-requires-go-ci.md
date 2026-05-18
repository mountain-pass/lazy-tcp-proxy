# Gate Docker Publish on Go CI Success

**Date Added**: 2026-05-18
**Priority**: High
**Status**: Planned

## Problem Statement

The `docker-publish` workflow (REQ-071) currently triggers directly on `push` to `main`. This means a Docker image could be built and published — and a GitHub release created — even if the Go CI checks (lint, vet, build, test) have not yet passed or have failed for that commit.

## Functional Requirements

1. The `docker-publish` workflow must not start until the `Go CI` workflow has completed **successfully** for the same commit on `main`.
2. If `Go CI` fails, `docker-publish` must not run at all for that commit.
3. The existing behaviour of `docker-publish` (multi-platform build, Docker Hub push, GitHub release) is unchanged.

## Technical Requirements

- Replace the `on.push` trigger in `.github/workflows/docker-publish.yml` with an `on.workflow_run` trigger that listens for the `Go CI` workflow completing on the `main` branch.
- Add a job-level `if` condition to skip the job when the upstream conclusion is not `success`.

## Acceptance Criteria

- [ ] Pushing to `main` when `Go CI` passes causes `docker-publish` to run.
- [ ] Pushing to `main` when `Go CI` fails causes `docker-publish` to be skipped.
- [ ] `docker-publish` does not trigger on pull-request runs of `Go CI` (scoped to `main` branch only).

## Dependencies

- Amends REQ-071 (`docker-publish` workflow).
- Depends on REQ-031 (`Go CI` workflow — the workflow name `"Go CI"` must match exactly).
