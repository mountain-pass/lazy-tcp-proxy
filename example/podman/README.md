# lazy-tcp-proxy — Podman

Podman exposes a Docker-compatible REST API via its socket service. Because lazy-tcp-proxy uses the Docker socket protocol, it works with Podman without any code changes — you only need to point the `DOCKER_SOCK` environment variable at the Podman socket.

## Prerequisites

- Podman 4.0 or later
- `podman-compose` or Docker Compose v2 (for the Compose example)

## Step 1: Enable the Podman socket

**Rootless (recommended):**

```bash
systemctl --user enable --now podman.socket
```

Verify the socket exists:

```bash
ls /run/user/$(id -u)/podman/podman.sock
```

**Root (system-wide):**

```bash
sudo systemctl enable --now podman.socket
# socket path: /run/podman/podman.sock
```

## Step 2: Run lazy-tcp-proxy

Pass the Podman socket path via `DOCKER_SOCK`:

```bash
podman run -d \
  -v /run/user/$(id -u)/podman/podman.sock:/var/run/docker.sock \
  -e DOCKER_SOCK=/var/run/docker.sock \
  -e IDLE_TIMEOUT_SECS=120 \
  -p "8080:8080" \
  -p "9000-9099:9000-9099" \
  --name lazy-tcp-proxy \
  mountainpass/lazy-tcp-proxy
```

> If running as root, replace the socket path with `/run/podman/podman.sock`.

## Step 3: Label your containers

Add these labels to any Podman container you want managed:

```bash
podman run -d \
  --label "lazy-tcp-proxy.enabled=true" \
  --label "lazy-tcp-proxy.ports=9001:80" \
  --name my-service \
  nginx
```

## Docker Compose example

`compose.yaml`:

```yaml
services:
  lazy-tcp-proxy:
    image: mountainpass/lazy-tcp-proxy
    restart: always
    volumes:
      - /run/user/${UID}/podman/podman.sock:/var/run/docker.sock
    environment:
      DOCKER_SOCK: /var/run/docker.sock
      IDLE_TIMEOUT_SECS: "120"
    ports:
      - "8080:8080"
      - "9001:9001"

  whoami:
    image: traefik/whoami
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "9001:80"
```

Run with:

```bash
podman-compose up -d
# or: docker compose up -d  (if using Docker Compose CLI against the Podman socket)
```

## Check status

```bash
curl http://localhost:8080/metrics
```

## Known limitations

- **Auto-start on socket activity**: Podman's socket service (`podman.socket`) uses systemd socket activation and may stop between uses in some configurations. Ensure the service is set to `enable` (not just `start`) so it persists across reboots.
- **Rootless networking**: In rootless mode, container-to-container networking uses a Podman-managed network. Ensure lazy-tcp-proxy and your managed containers share a common network so the proxy can reach them by internal IP.
- **SELinux**: On SELinux-enforcing systems, add `:z` to the socket volume mount: `-v .../podman.sock:/var/run/docker.sock:z`

For full label and environment variable reference, see the [main README](../../README.md).
