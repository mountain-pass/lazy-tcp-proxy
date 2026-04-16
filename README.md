
# lazy-tcp-proxy

# Overview

**On-demand TCP+UDP proxy for Docker containers.**

> 🥳 Now with UDP support! 🎉

## Introduction:

`lazy-tcp-proxy` allows you to run many Dockerized services on a single host, but only start containers when a connection arrives. It stops containers after a configurable idle timeout, saving resources while providing seamless access.

Supported architectures: `linux/amd64`, `linux/arm64`, `linux/arm/v7`

### Why:

To save compute resources (CPU, RAM, Electricity) on a single host by keeping containers stopped until they're actually needed, making it practical to run many low-traffic services without paying the cost of having them all running simultaneously.

### Feedback:

> "Finally, scale to zero!" - Nick G.

> "This is something that should really be built into Docker!" - Tom H.

---

## Quick Start

The quickest way to get started is to use the [docker-compose "recipes"](recipes).

These have many common services, with preconfigured options, so you can pick and choose.

(Don't forget to run [docker-compose.lazy-tcp-proxy.yml](recipes/docker-compose.lazy-tcp-proxy.yml))

Otherwise you can always run the container from the command line. You will need to add labels to your managed containers (see below).

```sh
docker run -d \
	-v /var/run/docker.sock:/var/run/docker.sock \
    -e IDLE_TIMEOUT_SECS=30 \
    -e POLL_INTERVAL_SECS=5 \
    -p "8080:8080" \
    -p "9000-9099:9000-9099" \
    --restart=always \
    --name lazy-tcp-proxy \
	mountainpass/lazy-tcp-proxy
```

---

## Container Label Configuration

Add these labels to any container you want proxied/managed:

| Label | Required | Description |
|-------|----------|-------------|
| `lazy-tcp-proxy.enabled` | Yes | Must be `true` to opt the container in |
| `lazy-tcp-proxy.ports` | Yes* | Comma-separated `<listen>:<target>` TCP port pairs |
| `lazy-tcp-proxy.udp-ports` | Yes* | Comma-separated `<listen>:<target>` UDP port pairs (see [UDP Support](#udp-support)) |
| `lazy-tcp-proxy.allow-list` | No | Comma-separated IPs/CIDRs. If set, only matching source addresses are forwarded; all others are silently dropped |
| `lazy-tcp-proxy.block-list` | No | Comma-separated IPs/CIDRs. If set, matching source addresses are silently dropped; all others are forwarded |
| `lazy-tcp-proxy.idle-timeout-secs` | No | Override the global `IDLE_TIMEOUT_SECS` for this container only (seconds). `0` = stop immediately when the last connection closes |
| `lazy-tcp-proxy.start-timeout-secs` | No | Override the global `START_TIMEOUT_SECS` for this container only (seconds). How long to wait for the upstream to respond to the first UDP datagram after a cold start before stopping the container and giving up |
| `lazy-tcp-proxy.webhook-url` | No | HTTP(S) URL to POST lifecycle events to (see [Webhooks](#webhooks)) |
| `lazy-tcp-proxy.dependants` | No | Comma-separated names of other managed containers/deployments that should start and stop alongside this one (see [Dependency Cascade](#dependency-cascade)) |
| `lazy-tcp-proxy.cron-start` | No | 5-field cron expression — start the container/deployment on this schedule (see [Cron Scheduling](#cron-scheduling)) |
| `lazy-tcp-proxy.cron-stop` | No | 5-field cron expression — stop the container/deployment on this schedule (see [Cron Scheduling](#cron-scheduling)) |
| `lazy-tcp-proxy.http-healthcheck` | No | URL to poll after a cold start — proxy waits for a 2xx response before forwarding TCP traffic. Supports `{{container}}` placeholder (see [HTTP Health Check](#http-health-check)) |

\* At least one of `lazy-tcp-proxy.ports` or `lazy-tcp-proxy.udp-ports` must be set. A container may use TCP only, UDP only, or both.

Both `allow-list` and `block-list` accept plain IP addresses (e.g. `127.0.0.1`, `::1`) and CIDR ranges (e.g. `192.168.0.0/16`, `fd00::/8`). If both labels are set, the allow-list is evaluated first. Blocked connections are logged with a red `(blocked)` suffix and do **not** wake the container.

Example:

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9000:80,9001:8080"
  - "lazy-tcp-proxy.allow-list=192.168.0.0/16,127.0.0.1"
  - "lazy-tcp-proxy.block-list=172.29.0.3,155.248.209.22"
```

---

## Container Self-Access

Some containers periodically poll their own endpoints — health checks, background sync tasks, keep-alive pings, etc. If that traffic routes through the proxy, it resets the idle timer and prevents the container from ever being stopped.

**Why this happens:**

When a container accesses itself via the Docker network gateway (e.g., `172.22.0.1:PORT`), the kernel source-NATs the packet. By the time the connection reaches the proxy, the source address is `172.22.0.1` — the proxy cannot distinguish the container talking to itself from any other host or container routing traffic through that same gateway.

**Three ways to prevent self-access from keeping a container alive:**

**1. Disable the keep-alive traffic in the application (ideal)**

The cleanest fix. Configure the application not to poll its own proxied ports. For example, point internal health checks at the container's direct port (e.g., `11434`) rather than the proxy's listen port (e.g., `9001`), or disable the polling entirely.

**2. Do not expose the keep-alive port via the proxy**

If only some ports need lazy startup, only include those in `lazy-tcp-proxy.ports` or `lazy-tcp-proxy.udp-ports`. A port that is not proxied cannot wake the container or reset the idle timer.

```yaml
labels:
  # Only proxy port 8080 — the internal health-check port 9090 is not listed
  - "lazy-tcp-proxy.ports=9000:8080"
```

**3. Block traffic from the gateway IP using `lazy-tcp-proxy.block-list`**

Add the Docker network's gateway IP to the container's block-list. Blocked connections are dropped before `EnsureRunning` is called, so they neither wake the container nor reset the idle timer.

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9001:11434"
  - "lazy-tcp-proxy.block-list=172.22.0.1"
```

> **Caveat:** The gateway IP is shared by the Docker host and by all containers on the same network that route through the gateway. For example, if another container accesses this service using `host.docker.internal` instead of the internal Docker network name, that traffic will also be blocked. For precise access control, prefer option 1 or 2, or reconfigure consumers to use internal Docker network addresses (e.g., `http://my-service:11434`) instead of going via the gateway.

The gateway IP varies by Docker network subnet. Find yours with:
```sh
docker network inspect <network-name> --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}'
```

---

## Environment Variables

| Variable              | Description                                                        | Default                   |
|-----------------------|--------------------------------------------------------------------|---------------------------|
| `IDLE_TIMEOUT_SECS`   | How long (in seconds) a container must be idle before being stopped. `0` = stop immediately once all connections close | 120  |
| `START_TIMEOUT_SECS`  | How long (in seconds) to wait for an upstream to be ready after a cold start — applies to the UDP datagram readiness probe, the HTTP health check (`lazy-tcp-proxy.http-healthcheck`), and the Docker HEALTHCHECK readiness gate. If the timeout is reached the connection/flow is dropped. Override per-container with the `lazy-tcp-proxy.start-timeout-secs` label | 30 |
| `POLL_INTERVAL_SECS`  | How often (in seconds) to check for idle containers                | 15                        |
| `DOCKER_SOCK`         | Path to Docker socket                                              | `/var/run/docker.sock`    |
| `STATUS_PORT`         | Port for the HTTP status server; set to `0` to disable            | 8080                      |

All are optional; defaults are safe for most setups.

---

## Status Endpoint

The proxy exposes a lightweight HTTP server for operational visibility.

### `GET /status`

Returns a JSON array of all currently managed containers and their state, sorted alphabetically by container name (then by container ID as a tie-breaker).

`last_active` shows when a container last handled traffic (falling back to the proxy start time if it has never been used). `last_active_relative` shows the same information in human-readable form, making it easy to spot long-idle containers at a glance — handy for identifying decommissioning candidates.

```sh
curl http://localhost:8080/status
```

```json
[
  {
    "container_id": "b2c3d4e5f6a1",
    "container_name": "idle-service",
    "listen_port": 9001,
    "target_port": 8080,
    "running": false,
    "active_conns": 0,
    "last_active": "2026-04-01T08:00:00Z",
    "last_active_relative": "3 days ago"
  },
  {
    "container_id": "a1b2c3d4e5f6",
    "container_name": "my-service",
    "listen_port": 9000,
    "target_port": 80,
    "running": true,
    "active_conns": 1,
    "last_active": "2026-04-01T12:34:56Z",
    "last_active_relative": "8 hours ago"
  }
]
```

### `GET /health`

Minimal liveness probe — always returns `200 ok` while the proxy is running.

```sh
curl http://localhost:8080/health
# ok
```

---

## UDP Support

The proxy can forward UDP datagrams in addition to TCP connections. Add the `lazy-tcp-proxy.udp-ports` label independently of (or alongside) `lazy-tcp-proxy.ports`.

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9000:80"        # TCP
  - "lazy-tcp-proxy.udp-ports=5353:53"    # UDP
```

**How it works:**

- The proxy binds a UDP socket on each declared listen port.
- The first datagram from a new client triggers `EnsureRunning` on the container (same as TCP).
- Each client is tracked as an independent *flow* (keyed by source IP + port). Responses from the container are routed back to the correct client.
- Flows idle for longer than `IDLE_TIMEOUT_SECS` are cleaned up automatically.
- The container is only stopped when **all** TCP connections **and** UDP flows are idle past the timeout.
- Allow-list and block-list labels apply to UDP traffic — datagrams from blocked addresses are silently dropped.

> **Note:** UDP is connectionless. The proxy uses one upstream socket per client flow, which suits the low-concurrency, lazy-start use case this proxy is designed for.

**Cold-start behaviour for slow UDP upstreams (e.g. Pi-hole):**

Some UDP upstreams (such as Pi-hole's DNS daemon) take several seconds to be
ready to handle datagrams after their container starts. The proxy handles this
with a shared readiness wait:

- When the first datagram arrives and the container is cold-starting, the
  proxy retries the datagram every 500 ms up to the `START_TIMEOUT_SECS`
  budget (default 30 s).
- Any additional datagrams that arrive from *other* clients while the retry
  loop is in progress are held and forwarded as soon as the upstream responds
  — they do **not** each start their own retry loop.
- If the upstream does not respond within `START_TIMEOUT_SECS`, the container
  is stopped cleanly and all pending datagrams are dropped. The next incoming
  datagram will trigger a fresh cold start.

Override the budget for a specific container with the
`lazy-tcp-proxy.start-timeout-secs` label:

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.udp-ports=53:53"
  - "lazy-tcp-proxy.start-timeout=30"   # seconds; default is START_TIMEOUT_SECS (30)
```

---

## HTTP Health Check

Some containers bind their service port during startup but aren't ready to handle requests yet (e.g. a database finishing migrations, or an app server loading configuration). The `lazy-tcp-proxy.http-healthcheck` label lets you declare a URL that the proxy will poll after starting the container, before forwarding any TCP traffic.

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=3306:3306"
  - "lazy-tcp-proxy.http-healthcheck=http://{{container}}:8080/health"
```

**How it works:**

- After `EnsureRunning` succeeds, the proxy polls the URL with `HTTP GET` every second.
- Any **2xx** response is treated as ready — proxying begins immediately.
- Non-2xx responses (e.g. `503 Service Unavailable`) and connection errors are both treated as "not yet ready" and retried.
- If no 2xx is received within `START_TIMEOUT_SECS` (default 30 s), the client connection is dropped and an error is logged. The next incoming connection will trigger a fresh cold-start attempt.
- When the label is absent, existing TCP behaviour is unchanged (the dial-retry loop handles port-level readiness).

**`{{container}}` placeholder:**

To avoid hardcoding internal IP addresses, use `{{container}}` in the URL — it is substituted with the container's IP address (Docker) or Service DNS name (Kubernetes) at connection time:

```yaml
# Both of these are equivalent for a container whose IP is 172.17.0.3:
lazy-tcp-proxy.http-healthcheck: "http://172.17.0.3:8080/health"
lazy-tcp-proxy.http-healthcheck: "http://{{container}}:8080/health"
```

> **Note:** Use `{{container}}` (double braces), not `${container}`. Single-dollar-brace syntax is interpreted by Docker Compose as a shell variable substitution and will be silently replaced with an empty string if the `container` environment variable is not set.

**Kubernetes annotation:**

```yaml
annotations:
  lazy-tcp-proxy.enabled: "true"
  lazy-tcp-proxy.ports: "3306:3306"
  lazy-tcp-proxy.http-healthcheck: "http://{{container}}:8080/health"
```

> **Note:** The HTTP health check applies to TCP connections only. UDP already has a protocol-native readiness probe (it retries the first datagram until the upstream responds).

---

## Docker HEALTHCHECK Readiness Gate

If a container ships with a Docker `HEALTHCHECK` instruction (or one is declared in a Compose file) and no `lazy-tcp-proxy.http-healthcheck` label is set, the proxy automatically waits for the container's health status to become `healthy` before forwarding TCP traffic. No configuration is required.

**Priority order** (first matching rule wins):

1. `lazy-tcp-proxy.http-healthcheck` label set → poll the declared URL (see [HTTP Health Check](#http-health-check))
2. Docker `HEALTHCHECK` present, no label → wait for `healthy` via Docker API
3. Neither → existing TCP dial-retry loop (unchanged)

**How it works:**

- After `EnsureRunning` succeeds, the proxy calls `ContainerInspect` every second and reads `State.Health.Status`.
- `healthy` → forwarding begins.
- `unhealthy` → the client connection is dropped immediately (no point retrying a container the daemon itself considers broken).
- `starting` → retried until `START_TIMEOUT_SECS` (default 30 s) is exhausted.
- Containers whose images have no `HEALTHCHECK` report status `none` — these fall through to the TCP dial-retry loop exactly as before.

**Example log output:**

```
proxy: docker-healthcheck: attempt 1: my-db → starting
proxy: docker-healthcheck: attempt 2: my-db → starting
proxy: docker-healthcheck: my-db healthy
```

**Zero configuration example (PostgreSQL):**

```yaml
services:
  db:
    image: postgres:16
    labels:
      - "lazy-tcp-proxy.enabled=true"
      - "lazy-tcp-proxy.ports=5432:5432"
    # The official postgres image ships with a HEALTHCHECK — no extra labels needed.
```

> **Note:** This feature is Kubernetes-transparent. The Kubernetes backend's `WaitUntilHealthy` is a no-op that returns immediately, and `HasHealthCheck` is always `false` for Kubernetes deployments. Kubernetes readiness probes are managed by the cluster, not the proxy.

---

## Webhooks

Containers can declare a webhook URL via the `lazy-tcp-proxy.webhook-url` label. The proxy will POST a JSON payload to that URL on the following events:

| Event | When | `connection_id` | `remote_addr` / `remote_port` | Usage metrics |
|-------|------|-----------------|-------------------------------|---------------|
| `container_started` | Proxy successfully started the container on an inbound connection | No | No | No |
| `container_stopped` | Proxy stopped the container due to idle timeout | No | No | No |
| `tcp_conn_start` | An inbound TCP connection was accepted (after allow/block-list check) | Yes | Yes | No |
| `tcp_conn_end` | That TCP connection has closed | Yes | Yes | Yes |
| `udp_flow_start` | A new UDP flow was established from a client (after allow/block-list check) | Yes | Yes | No |
| `udp_flow_end` | That UDP flow expired due to idle timeout | Yes | Yes | Yes |

**Common fields:**
- `connection_id` — UUID v4 shared by the start and end pair, allowing external systems to correlate events.
- `remote_addr` — client IP address (no port).
- `remote_port` — client port as an integer.
- `listen_port` — proxy listen port the client connected to (present on all connection/flow events).
- `started_at` — RFC3339 timestamp when the connection/flow was accepted (present on all connection/flow events).

**Usage metric fields** (present on `tcp_conn_end` and `udp_flow_end` only):
- `upstream_addr` — upstream `host:port` that was dialled.
- `ended_at` — RFC3339 timestamp when the connection/flow closed.
- `duration_ms` — wall-clock duration of the connection in milliseconds.
- `bytes_sent` — bytes transferred from client → upstream.
- `bytes_received` — bytes transferred from upstream → client.

**Container lifecycle payload** (`container_started` / `container_stopped`):
```json
{
  "event": "container_started",
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:34:56Z"
}
```

**TCP connection start payload** (`tcp_conn_start`):
```json
{
  "event": "tcp_conn_start",
  "connection_id": "550e8400-e29b-41d4-a716-446655440000",
  "remote_addr": "192.168.1.42",
  "remote_port": 54321,
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:34:56Z",
  "listen_port": 9000,
  "started_at": "2026-04-01T12:34:56Z"
}
```

**TCP connection end payload** (`tcp_conn_end`):
```json
{
  "event": "tcp_conn_end",
  "connection_id": "550e8400-e29b-41d4-a716-446655440000",
  "remote_addr": "192.168.1.42",
  "remote_port": 54321,
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:35:01Z",
  "listen_port": 9000,
  "upstream_addr": "172.17.0.3:80",
  "started_at": "2026-04-01T12:34:56Z",
  "ended_at": "2026-04-01T12:35:01Z",
  "duration_ms": 4823,
  "bytes_sent": 1024,
  "bytes_received": 204800
}
```

**UDP flow start payload** (`udp_flow_start`):
```json
{
  "event": "udp_flow_start",
  "connection_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "remote_addr": "192.168.1.42",
  "remote_port": 61234,
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:34:56Z",
  "listen_port": 5335,
  "started_at": "2026-04-01T12:34:56Z"
}
```

**UDP flow end payload** (`udp_flow_end`):
```json
{
  "event": "udp_flow_end",
  "connection_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "remote_addr": "192.168.1.42",
  "remote_port": 61234,
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:36:26Z",
  "listen_port": 5335,
  "upstream_addr": "172.17.0.4:53",
  "started_at": "2026-04-01T12:34:56Z",
  "ended_at": "2026-04-01T12:36:26Z",
  "duration_ms": 90000,
  "bytes_sent": 512,
  "bytes_received": 1024
}
```

Webhook calls are fire-and-forget with a 5-second timeout. Failures are logged as warnings and never affect proxying. If the label is absent, no webhook is fired.

**Example**:
```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9000:80"
  - "lazy-tcp-proxy.webhook-url=https://hooks.example.com/my-service"
```

---

## Cron Scheduling

Use `lazy-tcp-proxy.cron-start` and `lazy-tcp-proxy.cron-stop` to start and stop a container (or Kubernetes Deployment) on a fixed schedule. Both labels accept a standard **5-field cron expression** (`minute hour day-of-month month day-of-week`).

Either label may be set independently — you do not need both.

> **Note:** Containers with either cron label are **exempt from the idle-timeout inactivity checker**. They manage their own lifecycle via the schedule. The idle timer is still active for all other containers.

**Docker example — business hours only (Mon–Fri, 08:30–17:30):**

```yaml
services:
  my-db:
    image: postgres:16
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "5432:5432"
      lazy-tcp-proxy.cron-start: "30 8 * * 1-5"   # Start Mon–Fri at 08:30
      lazy-tcp-proxy.cron-stop:  "30 17 * * 1-5"  # Stop  Mon–Fri at 17:30
```

**Kubernetes example:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-db
  annotations:
    lazy-tcp-proxy.enabled: "true"
    lazy-tcp-proxy.ports: "5432:5432"
    lazy-tcp-proxy.cron-start: "30 8 * * 1-5"   # Start Mon–Fri at 08:30
    lazy-tcp-proxy.cron-stop:  "30 17 * * 1-5"  # Stop  Mon–Fri at 17:30
```

**Cron expression reference:**

```
┌─────────── minute        (0–59)
│ ┌───────── hour          (0–23)
│ │ ┌─────── day of month  (1–31)
│ │ │ ┌───── month         (1–12)
│ │ │ │ ┌─── day of week   (0–6, Sunday=0)
│ │ │ │ │
* * * * *
```

Common examples:

| Expression | Meaning |
|------------|---------|
| `30 8 * * 1-5` | 08:30 every weekday |
| `0 22 * * *` | 22:00 every day |
| `0 0 1 * *` | Midnight on the 1st of each month |

Schedules fire in the proxy's local timezone (UTC by default; set the `TZ` environment variable to override, e.g. `TZ=America/New_York`).

If the container is already in the desired state when a schedule fires (e.g. already running when `cron-start` triggers), the proxy logs the fact and takes no action.

---

## Dependency Cascade

Use `lazy-tcp-proxy.dependants` to declare a list of other managed containers
(or Kubernetes Deployments) that should start and stop automatically whenever
this container starts or stops.

**When to use it:** Hub-and-node patterns where the hub container acts as a
broker or event bus and the nodes are useless without it — for example, a
Selenium Grid hub with browser nodes.

```yaml
services:
  selenium-hub:
    image: selenium/hub:4.21.0
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "4444:4444"
      lazy-tcp-proxy.dependants: "selenium-chromium,selenium-firefox"

  selenium-chromium:
    image: selenium/node-chromium:4.21.0
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "5900:5900"
    environment:
      SE_EVENT_BUS_HOST: selenium-hub

  selenium-firefox:
    image: selenium/node-firefox:4.21.0
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "5901:5900"
    environment:
      SE_EVENT_BUS_HOST: selenium-hub
```

**Cascade rules:**
- When the hub starts (traffic arrives or external `docker start`), all listed
  dependants are started immediately.
- When the hub stops (idle timeout or external `docker stop`), all listed
  dependants are stopped.
- Values are the `ContainerName` / Deployment name of each managed dependant.
- If a dependant is already running/stopped, the cascade is a no-op.
- Works with both the Docker and Kubernetes images (use Deployment
  annotations instead of labels in k8s mode).

---

## Docker Engine Feature Request

This should be core functionality in the docker engine. As such, I've raised a Feature Request to add this behaviour - https://github.com/docker/roadmap/issues/899

---

## Questions and Answers

[Can be found here.](QANDA.md)

---

## Features

- **Automatic TCP proxying:** Listens on host ports and proxies to containers, starting them on demand.
- **Label-based configuration:** Opt-in containers using Docker labels—no static config files.
- **Multi-port support:** Proxy multiple ports per container using `lazy-tcp-proxy.ports` label.
- **Idle shutdown:** Containers are stopped after a configurable period of inactivity.
- **Dynamic discovery:** Watches Docker events for new/removed containers and updates proxy targets live.
- **Network auto-join:** Proxy joins Docker networks as needed to reach containers by internal IP.
- **Graceful shutdown:** Leaves all joined networks on SIGINT/SIGTERM.
- **Per-service IP filtering:** Optional allow-list and block-list per container via labels; supports plain IPs and CIDRs.
- **Structured, colorized logs:** Container names in yellow, network names in green, source addresses in cyan for easy scanning.

---

## Architecture

```mermaid
flowchart TD
  A([Incoming TCP Connection<br/>on Host Port]) -->|External Port| B[`lazy-tcp-proxy` Docker Container]
  B -->|Check target Container state| C{Target Container<br/> Running?}
  C -- No --> D([Start Target Container])
  C -- Yes --> E([Proxy Traffic])
  D --> E
  E -->|Internal Port/Network| F@{ shape: docs, label: "Target Docker Container/s"}
  F -- Idle Timeout --> G([Stop Target Docker Container])
  G -.->|Container Stopped| B
```

**How it works:**
- The proxy listens on host ports and intercepts incoming TCP connections.
- When a connection arrives, it checks if the target container is running (based on label configuration).
- If not running, it starts the container on demand.
- Proxies the connection to the container's internal port.
- If the container is idle for the configured timeout, it is stopped to save resources.

---

## Ideal Use Cases

Services that are accessed infrequently and can tolerate a few seconds of startup latency on the first connection. Good examples:

- **Home lab / self-hosted services** — a Minecraft server, Gitea, Jellyfin, or a personal wiki that only a handful of people use occasionally
- **Development environments** — per-branch or per-developer services that sit idle most of the day
- **Low-traffic internal tools** — dashboards, admin panels, CI artefact browsers that are visited a few times a day
- **Demo / staging environments** — services that need to be reachable on-demand but don't justify running 24/7

---

## Building and Publishing

```sh
cd lazy-tcp-proxy
VERSION=1.`date +%Y%m%d`.`git rev-parse --short=8 HEAD`
docker buildx build \
  --platform linux/amd64,linux/arm64/v8 \
  --tag mountainpass/lazy-tcp-proxy:${VERSION} \
  --tag mountainpass/lazy-tcp-proxy:latest \
  --push \
  .
```

---

## Required resources

The container is designed to run with an extremely low footprint.

```shell
CONTAINER ID   NAME               CPU %     MEM USAGE / LIMIT     MEM %     NET I/O           BLOCK I/O         PIDS
cbc5f775a793   lazy-tcp-proxy     0.00%     4.238MiB / 19.52GiB   0.02%     1.51MB / 1.4MB    0B / 0B           13
```

---

## Logging

- **Container names** are shown in yellow: `\033[33m<name>\033[0m`
- **Network names** are shown in green: `\033[32m<name>\033[0m`
- All key events (startup, discovery, container start/stop, network join/leave, proxy activity) are logged with clear, structured messages.
- Rejection reasons for misconfigured containers are logged on every start event.

---

## Requirements-First Development Workflow

All changes are tracked as requirements in the `requirements/` directory. See [AGENTS.md](AGENTS.md) for the full workflow. Every feature, fix, or change is documented and reviewed before implementation.

---

## Building & Development

- Written in Go, using the official Docker Go SDK.
- Minimal Docker image (`FROM scratch`).
- See requirements/ for detailed design and implementation notes.

---

## License

MIT