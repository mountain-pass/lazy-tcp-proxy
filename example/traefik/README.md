# Traefik + lazy-tcp-proxy Example

Demonstrates domain-name routing through Traefik to a lazy-started container.

```
User → Traefik (:80) → lazy-tcp-proxy (:9001) → whoami (started on demand)
```

Traefik polls `GET http://lazy-tcp-proxy:8080/traefik` every 5 seconds for routing
config. When a request arrives for `whoami.localhost`, Traefik forwards it to
lazy-tcp-proxy on port 9001, which starts the whoami container if it is idle.

## Start

```bash
docker compose up -d
```

## Test

Route a request via Traefik using the `Host` header:

```bash
curl -H "Host: whoami.localhost" http://localhost
```

You should see output from the whoami container (request details). The first
request will be slightly slower — lazy-tcp-proxy is starting the whoami container
on demand.

Wait 30 seconds with no traffic and the whoami container will stop automatically.
The next request starts it again.

## Check the generated Traefik config

```bash
curl http://localhost:8080/traefik | jq .
```

## Status dashboard

Open http://localhost:8080 in a browser to see the proxy status.

## Configuration

| Environment variable | Default | Description |
|---|---|---|
| `TRAEFIK_PROXY_HOST` | `lazy-tcp-proxy` | Hostname Traefik uses to reach lazy-tcp-proxy |
| `IDLE_TIMEOUT_SECS` | `120` | Seconds of inactivity before whoami is stopped |

To add more services, add them to `docker-compose.yml` with the labels:
```yaml
labels:
  - "lazy-tcp-proxy.enabled=true"
  - "lazy-tcp-proxy.ports=<listen_port>:<container_port>"
  - "lazy-tcp-proxy.traefik-hosts=<domain>:<listen_port>"
```

And expose the listen port from lazy-tcp-proxy:
```yaml
lazy-tcp-proxy:
  ports:
    - "<listen_port>:<listen_port>"
```
