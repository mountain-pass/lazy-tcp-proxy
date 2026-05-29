# Compose Re-provision on Missing Container

**Date Added**: 2026-05-29
**Priority**: Medium
**Status**: Planned

## Problem Statement

When a managed container is removed (e.g. by `docker system prune`, a manual `docker rm`,
or a host restart), the proxy marks it as `missing` and keeps its listeners alive.
On the next incoming connection, `EnsureRunning` calls `ContainerInspect` — which returns
a 404 because the container no longer exists — and the call fails. The connection is
dropped and the service is unavailable until the operator manually recreates the container.

If the operator has a Docker Compose file for the service, the proxy should be able to
re-provision the container automatically by running the equivalent of
`docker compose up -d <service>` when it detects the container is missing.

Additionally, some operators distribute their service as a custom Docker image in a
tar.gz archive (e.g. built offline or shipped as a file artefact). The proxy should
load such an image automatically before running Compose, so the operator only needs to
drop two files into the compose directory.

## Functional Requirements

1. The proxy looks for a Compose file in the **compose directory** (default:
   `/etc/lazy-tcp-proxy/compose/`) when `EnsureRunning` is called on a missing container.
2. The filename convention is `<service-name>.yml` with `.yaml` as a fallback.
3. Before running Compose `Up()`, the proxy checks for a tar.gz image archive at
   `<compose-dir>/<service-name>.tar.gz`. If present, it loads the image via the
   Docker API (equivalent of `docker load -i <name>.tar.gz`) before proceeding.
4. If a matching Compose file is found, the proxy calls the Docker Compose library's
   `Up()` operation (detached) for that service, effectively running
   `docker compose -f <file> up -d`.
5. If no Compose file is found, the proxy falls back to the existing error path
   (returns an error, connection is dropped) — no change in behaviour. A tar.gz
   without a matching Compose file is ignored.
5. The compose directory is derived automatically from `CONFIG_PATH`:
   - If `CONFIG_PATH=/etc/lazy-tcp-proxy/config.yaml`, compose dir = `/etc/lazy-tcp-proxy/compose/`
   - An explicit `COMPOSE_DIR` environment variable overrides the derived path.
6. After `Up()` succeeds, the existing `WatchEvents` listener picks up the container's
   `start` event and updates `running = true`, `missing = false` via the normal
   config-only container path — no additional plumbing is needed in the server.
7. Image loading (`<name>.tar.gz`) uses the existing `moby/moby/client` `ImageLoad`
   API — no new dependencies are required for this step.

## User Experience Requirements

- No user action is required beyond placing files in the compose directory.
- Operator drops up to two files per service:
  - `<name>.tar.gz` — optional custom image archive
  - `<name>.yml` (or `<name>.yaml`) — the Compose file
- The proxy logs clear messages at each step:
  - `docker: container "minio" missing — loading image from /etc/lazy-tcp-proxy/compose/minio.tar.gz`
  - `docker: container "minio" missing — re-provisioning via /etc/lazy-tcp-proxy/compose/minio.yml`
- If image loading or Compose re-provisioning fails, the error is logged and returned.

## Technical Requirements

- Use `github.com/docker/compose/v2/pkg/compose` and `github.com/docker/compose/v2/pkg/api`
  from the official Docker Compose v2 Go library.
- The compose file path search order: `<dir>/<name>.yml` then `<dir>/<name>.yaml`.
- The `Manager` struct gains a `composeDir string` field, set from the resolved compose
  directory at startup.
- Re-provisioning is added as a new private method `reprovisionWithCompose` on `Manager`,
  called from `EnsureRunning` when the inspect call returns a Docker "not found" error.
- The `containerBackend` interface (`internal/proxy/server.go`) is **not** changed;
  all compose logic lives entirely in the Docker backend.
- The Kubernetes backend is unaffected.

## Acceptance Criteria

- [ ] `Manager.EnsureRunning` detects a missing container (inspect 404) and checks for a
      Compose file in the compose directory.
- [ ] If `<name>.tar.gz` exists in the compose directory, the image is loaded via
      `m.cli.ImageLoad` before Compose `Up()` is called.
- [ ] If a Compose file exists, it runs Compose `Up()` (detached) and returns nil on success.
- [ ] If no Compose file exists, it returns the original "container not found" error unchanged.
      A tar.gz without a matching Compose file is silently ignored.
- [ ] After re-provisioning, `WatchEvents` picks up the `start` event and clears the
      `missing` flag automatically — confirmed by reading the `ContainerStarted` path.
- [ ] The compose directory defaults to `<dir of CONFIG_PATH>/compose/` and can be
      overridden via `COMPOSE_DIR`.
- [ ] Log messages are emitted for image loading and Compose re-provisioning.
- [ ] Build continues to pass (`go build ./...` and `golangci-lint`).

## Dependencies

- Adds `github.com/docker/compose/v2` (and transitively `github.com/docker/cli`) as a
  direct dependency — significant dependency weight (see prior discussion).
- Depends on REQ-065 (Dynamic Configuration File) for the `CONFIG_PATH` env var convention.
- Depends on REQ-080 (Fix: Config-Only Container Disappears After docker compose up) for
  the `WatchEvents` config-only container recovery path this feature relies on.

## Implementation Notes

- `ContainerInspect` returns an error containing "No such container" (Docker API 404).
  Use `errdefs.IsNotFound(err)` from `github.com/containerd/errdefs` (already an indirect
  dep) to identify the missing case cleanly.
- The compose library requires a `docker/cli` `command.DockerCli` instance. This can be
  constructed lightweight (no interactive terminal needed) using
  `command.NewDockerCli(command.WithStandardStreams())` and initialised with
  `cli.Initialize(flags.NewClientOptions())`.
- `Up()` should be called with `api.UpOptions{Create: api.CreateOptions{Recreate: api.RecreateDiverged}, Start: api.StartOptions{}}` so it behaves like `docker compose up -d`.
- Image loading uses `m.cli.ImageLoad(ctx, f, false)` where `f` is the opened tar.gz file.
  The response body must be drained and closed; errors in the response stream (not just
  the call itself) should be checked via `json.Decoder` on the response body.
- Image loading and Compose `Up()` are both gated on the compose file being present first;
  a lone tar.gz with no compose file does nothing.
