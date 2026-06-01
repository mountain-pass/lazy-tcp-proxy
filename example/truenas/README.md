# lazy-tcp-proxy — TrueNAS SCALE

TrueNAS SCALE 24.10 (Electric Eel) and later use Docker Compose as the app runtime, replacing the previous Kubernetes (k3s) backend. lazy-tcp-proxy works on Electric Eel and later without any special configuration — the standard Docker socket is available and no environment variable overrides are needed.

> **Minimum version:** TrueNAS SCALE 24.10 (Electric Eel). Earlier versions used k3s and are not covered by this guide.

## Prerequisites

- TrueNAS SCALE 24.10 (Electric Eel) or later
- Docker Apps enabled (Apps → Settings → Enable Apps)

## Deploying via Custom App

TrueNAS SCALE's **Custom App** option accepts a Docker Compose file directly.

1. In the TrueNAS web UI, go to **Apps** → **Discover Apps** → **Custom App**.

2. Select **Docker Compose** as the deployment method.

3. Paste the following Compose YAML, adjusting port ranges to match the services you want to proxy:

```yaml
services:
  lazy-tcp-proxy:
    image: mountainpass/lazy-tcp-proxy
    restart: always
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      IDLE_TIMEOUT_SECS: "120"
      POLL_INTERVAL_SECS: "15"
    ports:
      - "8080:8080"   # Status endpoint
      - "9001:9001"   # Example proxied service port (add more as needed)
    network_mode: host
```

> **`network_mode: host`** is recommended on TrueNAS SCALE so the proxy can reach managed containers on their internal IPs. Alternatively, use a shared Docker network (see [Networking](#networking) below).

4. Give the app a name (e.g. `lazy-tcp-proxy`) and click **Save**.

## Labelling your containers

Add the following labels to any container you want lazy-tcp-proxy to manage. In a Compose file:

```yaml
services:
  my-service:
    image: traefik/whoami
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "9001:80"
```

## Check status

Once the app is running, open a browser or run from the TrueNAS shell:

```bash
curl http://localhost:8080/metrics
```

From another machine on the network:

```bash
curl http://<truenas-ip>:8080/metrics
```

Managed containers that are stopped will show `"running": false` and will start automatically on first connection.

## Networking

TrueNAS SCALE runs all apps in Docker. Two networking options:

**Host networking (simplest):**
Add `network_mode: host` to the lazy-tcp-proxy service. The proxy shares the host network stack and can reach all containers by their Docker-assigned IPs.

**Bridge networking:**
Remove `network_mode: host` and ensure lazy-tcp-proxy and your managed containers share a Docker network:

```yaml
services:
  lazy-tcp-proxy:
    image: mountainpass/lazy-tcp-proxy
    networks:
      - proxy-net
    ...

  my-service:
    image: traefik/whoami
    networks:
      - proxy-net
    labels:
      lazy-tcp-proxy.enabled: "true"
      lazy-tcp-proxy.ports: "9001:80"

networks:
  proxy-net:
```

## Known limitations

- **TrueNAS SCALE pre-24.10**: Earlier versions (Dragonfish and prior) used k3s for apps. lazy-tcp-proxy has Kubernetes support but requires the separate `mountainpass/lazy-tcp-proxy-k8s` image and k8s manifests — see the [Kubernetes example](../kubernetes/README.md).
- **App isolation**: TrueNAS may apply additional namespacing or network policies for apps. If the proxy cannot reach a managed container, switching to `network_mode: host` usually resolves connectivity issues.
- **Socket permissions**: The Docker socket at `/var/run/docker.sock` is owned by root on TrueNAS. The lazy-tcp-proxy image runs as root by default and can access it without extra configuration.

For full label and environment variable reference, see the [main README](../../README.md).
