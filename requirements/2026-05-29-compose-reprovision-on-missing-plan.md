# Compose Re-provision on Missing Container — Implementation Plan

**Requirement**: [2026-05-29-compose-reprovision-on-missing.md](2026-05-29-compose-reprovision-on-missing.md)
**Date**: 2026-05-29
**Status**: Draft

---

## Implementation Steps

1. **Add `github.com/docker/compose/v2` dependency**
   Run `go get github.com/docker/compose/v2` inside `lazy-tcp-proxy/` to update `go.mod` and `go.sum`.

2. **Add `SetComposeDir` to the `backendManager` interface** (`main.go`)
   Append one method to the interface so the compose directory can be set on any backend at startup:
   ```go
   SetComposeDir(dir string)
   ```

3. **Add no-op `SetComposeDir` to the Kubernetes backend** (`internal/k8s/backend.go`)
   ```go
   // SetComposeDir is a no-op for the k8s backend (compose re-provisioning is Docker-only).
   func (b *Backend) SetComposeDir(_ string) {}
   ```

4. **Add `composeDir` field and `SetComposeDir` to Docker `Manager`** (`internal/docker/manager.go`)
   Add `composeDir string` to the `Manager` struct and implement:
   ```go
   func (m *Manager) SetComposeDir(dir string) { m.composeDir = dir }
   ```

5. **Add `resolveComposeDir` and wire it in `main.go`**
   New helper function:
   ```go
   func resolveComposeDir(configPath string) string {
       if v := os.Getenv("COMPOSE_DIR"); v != "" {
           return v
       }
       return filepath.Join(filepath.Dir(configPath), "compose")
   }
   ```
   Called immediately after `resolveConfigPath()` in `main()`:
   ```go
   configPath := resolveConfigPath()
   mgr.SetComposeDir(resolveComposeDir(configPath))
   ```
   Log the resolved directory:
   ```go
   log.Printf("compose dir: %s (set COMPOSE_DIR to override)", resolveComposeDir(configPath))
   ```

6. **Create `internal/docker/compose_reprovision.go`**
   New file with two private methods on `*Manager`:

   **`loadImageFromTar(ctx, name, tarPath string) error`**
   - Opens `tarPath` with `os.Open`
   - Calls `m.cli.ImageLoad(ctx, f, client.ImageLoadOptions{})` (or equivalent moby API)
   - Drains the response body (`json.Decoder` loop) to check for stream errors
   - Logs progress: `docker: loading image for \033[33m%s\033[0m from %s`

   **`reprovisionWithCompose(ctx, name string) error`**
   - Resolves the compose file path: try `<composeDir>/<name>.yml`, then `<composeDir>/<name>.yaml`
   - Returns `nil` immediately (no error, no reprovision) if no file found — `EnsureRunning` will surface its own "not found" error
   - If found, checks for `<composeDir>/<name>.tar.gz`; if present calls `loadImageFromTar`
   - Constructs a `command.DockerCli` (no interactive terminal):
     ```go
     dockerCLI, _ := command.NewDockerCli()
     _ = dockerCLI.Initialize(flags.NewClientOptions())
     ```
   - Loads the compose project:
     ```go
     opts, _ := cli.NewProjectOptions(
         []string{composeFilePath},
         cli.WithWorkingDirectory(filepath.Dir(composeFilePath)),
         cli.WithName(name),
     )
     project, _ := cli.ProjectFromOptions(ctx, opts)
     ```
   - Runs compose Up (detached — no Attach writer):
     ```go
     composeSvc := compose.NewComposeService(dockerCLI)
     err = composeSvc.Up(ctx, project, api.UpOptions{
         Create: api.CreateOptions{Recreate: api.RecreateDiverged},
     })
     ```
   - Returns any error from `Up()`

7. **Modify `EnsureRunning` in `manager.go`**
   After the existing swarm-service early-return, change the inspect error path:
   ```go
   result, err := m.cli.ContainerInspect(ctx, targetID, ...)
   if err != nil {
       if errdefs.IsNotFound(err) && m.composeDir != "" {
           return m.reprovisionWithCompose(ctx, targetID)
       }
       return fmt.Errorf("inspecting container: %w", err)
   }
   ```
   `errdefs` is already an indirect dependency (`github.com/containerd/errdefs`).

8. **Update `README.md`**
   Add a new section **"Compose Re-provisioning"** (between the `docker system prune` warning
   and the Features section) and add the `COMPOSE_DIR` variable to the Environment Variables table.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/go.mod` | Modify | Add `github.com/docker/compose/v2` direct dependency |
| `lazy-tcp-proxy/go.sum` | Modify | Updated by `go get` |
| `lazy-tcp-proxy/main.go` | Modify | Add `SetComposeDir` to `backendManager` interface; add `resolveComposeDir`; wire at startup |
| `lazy-tcp-proxy/internal/k8s/backend.go` | Modify | Add no-op `SetComposeDir` |
| `lazy-tcp-proxy/internal/docker/manager.go` | Modify | Add `composeDir` field, `SetComposeDir` method; modify `EnsureRunning` |
| `lazy-tcp-proxy/internal/docker/compose_reprovision.go` | Create | `reprovisionWithCompose` and `loadImageFromTar` methods |
| `README.md` | Modify | New "Compose Re-provisioning" section + env var table row |

---

## API Contracts

No new HTTP endpoints. The feature is entirely internal to the proxy startup path.

---

## Key Code Snippets

### Compose file discovery (in `reprovisionWithCompose`)
```go
func (m *Manager) findComposeFile(name string) string {
    for _, ext := range []string{".yml", ".yaml"} {
        p := filepath.Join(m.composeDir, name+ext)
        if _, err := os.Stat(p); err == nil {
            return p
        }
    }
    return ""
}
```

### Log messages
```
docker: container "minio" missing — loading image from /etc/lazy-tcp-proxy/compose/minio.tar.gz
docker: image loaded for "minio"
docker: container "minio" missing — re-provisioning via /etc/lazy-tcp-proxy/compose/minio.yml
docker: container "minio" re-provisioned successfully
```

### README section (new)
```markdown
## Compose Re-provisioning

If a config-only container is removed (by `docker system prune`, `docker rm`, or a host
restart), the proxy can re-provision it automatically the next time a connection arrives,
using a Docker Compose file you provide.

**Convention:** place two files in the compose directory (default `/etc/lazy-tcp-proxy/compose/`):

| File | Purpose |
|------|---------|
| `<name>.yml` (or `<name>.yaml`) | Docker Compose file for the service |
| `<name>.tar.gz` | *(optional)* Custom image archive loaded before `compose up` |

**Example** — for a service named `minio`:
- `/etc/lazy-tcp-proxy/compose/minio.yml`
- `/etc/lazy-tcp-proxy/compose/minio.tar.gz` *(if the image is distributed as a file)*

When a connection arrives and the container is missing, the proxy will:
1. Load `minio.tar.gz` into Docker (equivalent of `docker load -i minio.tar.gz`), if present
2. Run `docker compose up -d` using `minio.yml`

If neither file exists, the proxy returns an error as before — no behaviour change.

Configure via:

| Variable | Default | Description |
|----------|---------|-------------|
| `COMPOSE_DIR` | `<dir of CONFIG_PATH>/compose` | Directory scanned for compose files and image archives |
```

---

## Unit Tests

No new unit tests are added in this change (compose library integration requires a live
Docker socket). The acceptance criteria are verified manually by:

| Test | Input | Expected Output |
|------|-------|-----------------|
| Missing container, compose file present | `EnsureRunning("minio")` when `minio` container absent, `minio.yml` exists | `Up()` called, nil returned |
| Missing container, compose file absent | `EnsureRunning("minio")` when `minio` container absent, no compose files | Original "not found" error returned |
| Missing container, tar.gz + compose both present | Both files in compose dir | `ImageLoad` called first, then `Up()` |
| Tar.gz present but no compose file | Only `minio.tar.gz` in compose dir | No action taken (compose file required to gate) |
| Label-discovered container gone (no compose file) | `EnsureRunning("<64-char-hex>")` | No compose file found, original error returned |

---

## Risks & Open Questions

1. **Compose library API surface**: The exact method signatures of `cli.NewProjectOptions`,
   `cli.ProjectFromOptions`, and `api.UpOptions` must be verified against the version of
   `github.com/docker/compose/v2` that `go get` resolves. Adjust snippets in step 6 as needed.

2. **`ImageLoad` API in moby/moby/client**: The `moby/moby/client` API differs slightly from
   `docker/docker/client`. Verify the `ImageLoad` call signature against `v0.4.0` during
   implementation.

3. **Dependency size**: Adding `docker/compose/v2` roughly doubles the module graph. Docker and
   BuildKit internals become transitive dependencies. The final binary size increase should be
   noted after the build.

4. **`WatchEvents` picks up re-provisioned container**: After `Up()`, the new container fires
   a `start` event. The config-only path in `WatchEvents` calls `ContainerStarted(rid)` using
   the registered name, clearing `missing = true` and setting `running = true`. This is the
   existing recovery path — no change needed. Verify it still works after the compose re-provision.
