# lazy-tcp-proxy — Docker Example

This example runs lazy-tcp-proxy alongside five on-demand services using Docker Compose. All five services start at **zero running containers** and are started automatically the first time a connection arrives.

## Services

| Service | Protocol | Port | Notes |
|---------|----------|------|-------|
| whoami | TCP | 9001 | HTTP echo (headers, IP, etc.) |
| openssh-server | TCP | 9002 | SSH — user `admin`, password `password` |
| postgres | TCP | 5432 | User `admin`, password `password`, DB `mydatabase` |
| mongo | TCP | 27017 | User `admin`, password `password`, DB `mydatabase` |
| udp-echo | UDP | 9003 | Echoes any datagram back to the sender |

The idle timeout is **30 seconds** — a service stops automatically 30 seconds after the last connection closes.

## Prerequisites

- Docker and Docker Compose v2

## Start the example

```bash
cd example/docker
docker compose up -d
```

The proxy starts immediately. All target containers remain stopped until first use.

Check the proxy status (all services should show `running: false`):

```bash
curl -s http://localhost:8080/metrics | python3 -m json.tool
```

## Trigger on-demand scaling (0 → 1)

Each command below makes a connection through the proxy. Watch the container start in real time with `docker compose logs -f lazy-tcp-proxy`.

**whoami (HTTP):**
```bash
curl http://localhost:9001
```

**openssh-server (SSH):**
```bash
ssh admin@localhost -p 9002
# password: password
# type 'exit' to close the session
```

**postgres:**
```bash
psql -h localhost -p 5432 -U admin -d mydatabase
# password: password
# type '\q' to exit
```

**mongo:**
```bash
mongosh "mongodb://admin:password@localhost:27017/mydatabase?authSource=admin"
# type 'exit' to close
```

**udp-echo:**
```bash
echo "hello" | nc -u -w1 localhost 9003
# The first datagram starts the container; the server echoes it back.
```

## What to look for

**Proxy logs** — watch a container start on first connection:
```bash
docker compose logs -f lazy-tcp-proxy
```

You will see lines like:
```
proxy: new connection to whoami (port 80) from 172.x.x.x:xxxxx
docker: starting container whoami
docker: container whoami started
proxy: proxying connection to 172.x.x.x:80
proxy: last connection to whoami closed; idle timer started (container will stop in ~30s)
docker: stopping container whoami (idle timeout)
```

**Status endpoint** — poll to observe running state change:
```bash
watch -n2 'curl -s http://localhost:8080/metrics | python3 -m json.tool'
```

The `running` field flips from `false` → `true` on first connection and back to `false` after the idle timeout.

## Shutdown

```bash
docker compose down
```

Add `-v` to also remove the persistent volumes (postgres and mongo data):
```bash
docker compose down -v
```
