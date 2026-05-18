# Label & Annotation Configuration

lazy-tcp-proxy is configured by adding labels (Docker) or annotations (Kubernetes) to the
containers and Deployments you want managed.

> **Alternative:** If you cannot add labels to an existing container, or want to manage
> configuration centrally without recreating containers, see [README_CONFIG.md](README_CONFIG.md)
> for the dynamic YAML config file.

---

## Label Reference

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
| `lazy-tcp-proxy.tls` | No | Set to `true` to wrap the listener with TLS using a shared self-signed certificate. Works with any TCP protocol, not only HTTP (see [TLS Termination](#tls-termination)) |
| `lazy-tcp-proxy.api-key` | No | Require clients to send this value in the `X-API-Key` header on every HTTP request. Missing or incorrect key → `401 Unauthorized`. The header is stripped before forwarding (see [API Key Authentication](#api-key-authentication)) |

\* At least one of `lazy-tcp-proxy.ports` or `lazy-tcp-proxy.udp-ports` must be set. A container may use TCP only, UDP only, or both.

Both `allow-list` and `block-list` accept plain IP addresses (e.g. `127.0.0.1`, `::1`) and CIDR ranges (e.g. `192.168.0.0/16`, `fd00::/8`). If both labels are set, the allow-list is evaluated first. Blocked connections are logged with a red `(blocked)` suffix and do **not** wake the container.

**Minimal example:**

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9000:80,9001:8080"
```

**With allow/block lists:**

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

Configure the application not to poll its own proxied ports. For example, point internal health checks at the container's direct port (e.g., `11434`) rather than the proxy's listen port (e.g., `9001`), or disable the polling entirely.

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

> **Caveat:** The gateway IP is shared by the Docker host and by all containers on the same network that route through the gateway. For precise access control, prefer option 1 or 2, or reconfigure consumers to use internal Docker network addresses (e.g., `http://my-service:11434`) instead of going via the gateway.

The gateway IP varies by Docker network subnet. Find yours with:
```sh
docker network inspect <network-name> --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}'
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

Some UDP upstreams (such as Pi-hole's DNS daemon) take several seconds to be ready to handle datagrams after their container starts. The proxy handles this with a shared readiness wait:

- When the first datagram arrives and the container is cold-starting, the proxy retries the datagram every 500 ms up to the `START_TIMEOUT_SECS` budget (default 30 s).
- Any additional datagrams that arrive from *other* clients while the retry loop is in progress are held and forwarded as soon as the upstream responds — they do **not** each start their own retry loop.
- If the upstream does not respond within `START_TIMEOUT_SECS`, the container is stopped cleanly and all pending datagrams are dropped. The next incoming datagram will trigger a fresh cold start.

Override the budget for a specific container with the `lazy-tcp-proxy.start-timeout-secs` label:

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.udp-ports=53:53"
  - "lazy-tcp-proxy.start-timeout-secs=30"   # seconds; default is START_TIMEOUT_SECS (30)
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

| Event | When | `connection_id` | `remote_addr` / `remote_port` |
|-------|------|-----------------|-------------------------------|
| `container_started` | Proxy successfully started the container on an inbound connection | No | No |
| `container_stopped` | Proxy stopped the container due to idle timeout | No | No |
| `tcp_conn_start` | An inbound TCP connection was accepted (after allow/block-list check) | Yes | Yes |
| `tcp_conn_end` | That TCP connection has closed | Yes | Yes |
| `udp_flow_start` | A new UDP flow was established from a client (after allow/block-list check) | Yes | Yes |
| `udp_flow_end` | That UDP flow expired due to idle timeout | Yes | Yes |

- `connection_id` — UUID v4 shared by the start and end pair, allowing external systems to correlate them and measure duration.
- `remote_addr` — client IP address (no port).
- `remote_port` — client port as an integer.

**Container lifecycle payload** (`container_started` / `container_stopped`):
```json
{
  "event": "container_started",
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:34:56Z"
}
```

**TCP connection payload** (`tcp_conn_start` / `tcp_conn_end`):
```json
{
  "event": "tcp_conn_start",
  "connection_id": "550e8400-e29b-41d4-a716-446655440000",
  "remote_addr": "192.168.1.42",
  "remote_port": 54321,
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:34:56Z"
}
```

**UDP flow payload** (`udp_flow_start` / `udp_flow_end`):
```json
{
  "event": "udp_flow_start",
  "connection_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "remote_addr": "192.168.1.42",
  "remote_port": 61234,
  "container_id": "a1b2c3d4e5f6",
  "container_name": "my-service",
  "timestamp": "2026-04-01T12:34:56Z"
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
    lazy-tcp-proxy.cron-start: "30 8 * * 1-5"
    lazy-tcp-proxy.cron-stop:  "30 17 * * 1-5"
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

Use `lazy-tcp-proxy.dependants` to declare a list of other managed containers (or Kubernetes Deployments) that should start and stop automatically whenever this container starts or stops.

**When to use it:** Hub-and-node patterns where the hub container acts as a broker or event bus and the nodes are useless without it — for example, a Selenium Grid hub with browser nodes.

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
- When the hub starts (traffic arrives or external `docker start`), all listed dependants are started immediately.
- When the hub stops (idle timeout or external `docker stop`), all listed dependants are stopped.
- Values are the `ContainerName` / Deployment name of each managed dependant.
- If a dependant is already running/stopped, the cascade is a no-op.
- Works with both the Docker and Kubernetes images (use Deployment annotations instead of labels in k8s mode).

---

## TLS Termination

Set `lazy-tcp-proxy.tls=true` to wrap the container's listener with TLS. The proxy generates a **shared self-signed ECDSA (P-256) certificate** at startup (valid for 10 years) and uses it for every TLS-enabled container.

Because TLS is applied at the TCP listener level (via `tls.NewListener`), it works with **any TCP protocol** — HTTP, MySQL, Redis, MQTT, custom protocols, etc. — not only HTTP.

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9443:8080"
  - "lazy-tcp-proxy.tls=true"
```

Clients must connect with TLS (e.g. `https://host:9443`) and accept the self-signed certificate. Most HTTP clients can be told to skip certificate verification:

```sh
curl -k https://localhost:9443/
```

> **Note:** The self-signed certificate is regenerated on every proxy restart. It has no Subject Alternative Names (SANs) and no configured minimum TLS version — suitable for development and internal use. For production, consider terminating TLS at a reverse proxy (e.g. Traefik, nginx) instead.

**Kubernetes annotation:**

```yaml
annotations:
  lazy-tcp-proxy.enabled: "true"
  lazy-tcp-proxy.ports: "9443:8080"
  lazy-tcp-proxy.tls: "true"
```

---

## API Key Authentication

Set `lazy-tcp-proxy.api-key` to require an `X-API-Key` header on every **HTTP** request forwarded to the container.

- If the header is missing or its value does not match the label exactly → the proxy responds with `HTTP/1.1 401 Unauthorized` and closes the connection.
- If the header matches → the proxy strips it before forwarding the request to the container (the upstream never sees `X-API-Key`).
- HTTP/1.1 keep-alive is honoured: subsequent requests on the same connection are each checked independently.

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9000:80"
  - "lazy-tcp-proxy.api-key=my-secret-key"
```

**Client example:**

```sh
# Correct key — request is forwarded
curl -H "X-API-Key: my-secret-key" http://localhost:9000/

# Missing key — 401 Unauthorized
curl http://localhost:9000/
```

**Combined TLS + API Key:**

Both labels can be set together to get encrypted and authenticated access:

```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=9443:8080"
  - "lazy-tcp-proxy.tls=true"
  - "lazy-tcp-proxy.api-key=my-secret-key"
```

```sh
curl -k -H "X-API-Key: my-secret-key" https://localhost:9443/
```

> **Note:** `lazy-tcp-proxy.api-key` is HTTP-specific. If you enable it on a port serving a non-HTTP protocol (MySQL, Redis, etc.), the first bytes from the client will not parse as an HTTP request and the connection will be closed immediately.

**Kubernetes annotation:**

```yaml
annotations:
  lazy-tcp-proxy.enabled: "true"
  lazy-tcp-proxy.ports: "9000:80"
  lazy-tcp-proxy.api-key: "my-secret-key"
```
