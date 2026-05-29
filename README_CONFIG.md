# Dynamic Configuration File

The dynamic config file lets you configure the proxy **without labels** — useful when:

- You have existing containers that can't be recreated to add labels.
- You want centralised configuration separate from the containers themselves.
- You want to add proxy targets for containers that have no `lazy-tcp-proxy` labels.

Configuration is stored in a YAML file on disk. Changes are applied by calling
`GET /config/reload` on the admin API, or by writing a new config via `PUT /config/update`.

> **Label-based configuration** still works exactly as before and is documented in
> [README_LABELS.md](README_LABELS.md). When a container appears in both the YAML file and has
> Docker labels, **the YAML config wins entirely** — labels are ignored for that container.

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CONFIG_PATH` | `/etc/lazy-tcp-proxy/config.yaml` | Path to the YAML config file |
| `ADMIN_PORT` | `0` | Port for the admin API server. Set to `0` to disable (default) |
| `ADMIN_API_KEY` | *(required if `ADMIN_PORT` > 0)* | API key for authenticating admin API requests |
| `WEB_PORT` | `8080` | Port for the HTTP web server (dashboard, `/metrics`, `/traefik`, `/health`); `STATUS_PORT` is a legacy alias |
| `WEB_HOST` | *(none)* | When set, adds `Host('<WEB_HOST>') → http://<TRAEFIK_PROXY_HOST>:<WEB_PORT>` to `/traefik`, exposing the web endpoint via Traefik |
| `TRAEFIK_PROXY_HOST` | `lazy-tcp-proxy` | Hostname/IP Traefik uses to reach lazy-tcp-proxy's listen ports (see [Traefik Integration](README_LABELS.md#traefik-integration)) |
| `TRAEFIK_ENTRYPOINT` | `websecure` | Traefik entry point name added to every generated router's `entryPoints`; set to `""` to omit |
| `TRAEFIK_CERTRESOLVER` | `myresolver` | Cert resolver name added to every generated router's `tls.certResolver`; set to `""` to omit |

If `ADMIN_PORT` is non-zero and `ADMIN_API_KEY` is not set, the proxy will refuse to start.

---

## Config File

### Location

By default the proxy reads `/etc/lazy-tcp-proxy/config.yaml`. Override with `CONFIG_PATH`.

If the file does not exist at startup, a placeholder file is created automatically and a
warning is logged. The placeholder contains a commented-out example of every available option
so you can uncomment and edit what you need:

```yaml
services:
#  - name: "my-container"
#    ports:
#      - "9000:80"
#    udp_ports:
#      - "5353:53"
#    allow_list: ["192.168.0.0/24"]
#    block_list: ["10.0.0.1"]
#    idle_timeout_secs: 60
#    start_timeout_secs: 30
#    webhook_url: "https://example.com/hook"
#    dependants: ["other-service"]
#    cron_start: "0 9 * * 1-5"
#    cron_stop:  "0 17 * * 1-5"
#    availability: "ondemand"   # ondemand (default), cron, or manual
#    http_healthcheck: "http://{{container}}:8080/health"
#    tls: true
#    api_key:
#      - "your-secret-key"
#    basic_auth:
#      - "user:password"
#    traefik_hosts:
#      - "myapp.localhost:9000"
#    traefik_tcp_hosts:
#      - "mongo.localhost:27015"
```

### Docker Compose setup

Mount a host directory or named volume at the config path and pass `CONFIG_PATH`:

```yaml
services:
  lazy-tcp-proxy:
    image: mountainpass/lazy-tcp-proxy
    volumes:
      - /host/path/to/config:/config
    environment:
      CONFIG_PATH: /config/config.yaml
      ADMIN_PORT: "8081"
      ADMIN_API_KEY: "your-secret-key"
    ports:
      - "8080:8080"   # status UI
      - "8081:8081"   # admin API (only expose if needed)
      - "9000-9099:9000-9099"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

### YAML schema

All fields except `name` are optional.

```yaml
services:
  - name: "my-container"          # required — matches the Docker container name or K8s Deployment name
    ports:
      - "9000:80"                 # listen:target TCP port pairs (same format as the label)
    udp_ports:
      - "5353:53"                 # listen:target UDP port pairs
    allow_list:
      - "192.168.0.0/24"          # only forward traffic from these IPs/CIDRs
      - "10.0.0.1"
    block_list:
      - "172.29.0.3"              # drop traffic from these IPs/CIDRs
    idle_timeout_secs: 60         # override global IDLE_TIMEOUT_SECS for this service
    start_timeout_secs: 30        # override global START_TIMEOUT_SECS for this service
    webhook_url: "https://example.com/hook"
    dependants:
      - "other-service"           # cascade start/stop to these containers
    cron_start: "0 9 * * 1-5"    # start Mon–Fri at 09:00
    cron_stop:  "0 17 * * 1-5"   # stop  Mon–Fri at 17:00
    availability: "ondemand"     # ondemand (default), cron (schedule-only), or manual (passthrough)
    http_healthcheck: "http://{{container}}:8080/health"
    tls: true                     # wrap listener with TLS (shared self-signed cert)
    api_key:                      # require X-API-Key header matching any listed value
      - "your-secret-key"
    basic_auth:                   # require Authorization: Basic matching any listed user:password
      - "nick:somepassword"
    traefik_hosts:
      - "myapp.localhost:9000"    # domain:listen_port pairs for Traefik HTTP provider
    traefik_tcp_hosts:
      - "mongo.localhost:9001"    # domain:listen_port pairs for Traefik TCP SNI routing
```

See [README_LABELS.md](README_LABELS.md) for full descriptions of each field — the YAML fields
map 1-to-1 with Docker labels (e.g. `ports` = `lazy-tcp-proxy.ports`, `allow_list` =
`lazy-tcp-proxy.allow-list`, etc.).

### Override vs. labels

When a `name` in the YAML config matches a discovered Docker container or Kubernetes Deployment,
the **entire YAML entry replaces** that container's label-derived configuration. There is no
field-level merging — if the YAML entry is present, labels are ignored completely for that service.

```
Docker labels only  →  label config is used
YAML only           →  YAML config is used; container ID resolved by name
Both present        →  YAML config wins entirely; labels ignored
```

### Containers without labels

A YAML entry whose `name` does not match any labeled container is registered as a new proxy
target. The proxy looks up the container by name via the Docker API (or Kubernetes API) when a
connection arrives — no `lazy-tcp-proxy.enabled` label is needed.

```yaml
services:
  # This container has no lazy-tcp-proxy labels at all.
  - name: "my-unlabelled-container"
    ports:
      - "9050:8080"
    idle_timeout_secs: 120
```

> **Note:** Unlabelled containers are not picked up automatically when they start or restart.
> Call `GET /config/reload` after starting such a container to register it with the proxy.

---

## Admin API

The admin API runs on `ADMIN_PORT` (default `0`, disabled) and is protected by the `X-API-Key` header.

All requests must include:
```
X-API-Key: <value of ADMIN_API_KEY>
```

Missing or incorrect key → `401 Unauthorized`.

### `GET /config`

Returns the current in-memory config as JSON.

```sh
curl -H "X-API-Key: your-secret-key" http://localhost:8081/config
```

```json
{
  "services": [
    {
      "name": "my-container",
      "ports": ["9000:80"],
      "idle_timeout_secs": 60
    }
  ]
}
```

### `GET /config/reload`

Re-reads the YAML file from disk and re-applies it to the running proxy. Use this after editing
the file directly.

```sh
curl -H "X-API-Key: your-secret-key" http://localhost:8081/config/reload
```

```json
{ "status": "ok", "services_loaded": 2 }
```

On error (e.g. file not readable):
```json
{ "status": "error", "error": "config: read /etc/lazy-tcp-proxy/config.yaml: ..." }
```

### `PUT /config/update`

Accepts a JSON body in the same shape as `GET /config`, overwrites the YAML file on disk,
and re-applies the config to the running proxy. The file is written atomically (write to a
temp file, then rename).

```sh
curl -X PUT \
  -H "X-API-Key: your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "services": [
      {
        "name": "my-container",
        "ports": ["9000:80"],
        "idle_timeout_secs": 60
      }
    ]
  }' \
  http://localhost:8081/config/update
```

```json
{ "status": "ok", "services_loaded": 1 }
```

If the JSON is invalid or a service entry fails validation, the file is **not** modified and the
running config is unchanged:

```json
{ "status": "error", "error": "service \"foo\": must specify at least one port in ports or udp_ports" }
```

---

## Reload behaviour

When a reload is triggered (startup, `GET /config/reload`, or `PUT /config/update`), the proxy:

1. Re-runs backend discovery (`Discover()`) to get the current set of labeled containers.
2. Applies the YAML overlay — matching entries replace label config; unmatched entries are added.
3. Reconciles the running proxy:
   - Targets no longer in the merged list are removed (listeners closed).
   - New targets are registered (listeners opened).
   - Changed targets are re-registered (brief port-unbound window of a few milliseconds).
   - Unchanged targets are left untouched (no listener disruption).

Active connections are not interrupted during reload unless the target they belong to is removed or its listen port changes.
