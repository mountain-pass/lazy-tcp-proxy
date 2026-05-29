# lazy-tcp-proxy — Load Test Example

End-to-end load tests for the TCP and UDP proxy using [`iperf3`](https://github.com/esnet/iperf).
All traffic is routed **through the proxy**, so results reflect real proxy overhead including
lazy container startup on the first connection.

## Architecture

```
[iperf3 client]  ──TCP 5201──►  [lazy-tcp-proxy]  ──TCP──►  [iperf3-tcp container]
[iperf3 client]  ──UDP 5202──►  [lazy-tcp-proxy]  ──UDP──►  [iperf3-udp container]
                 ──TCP 5202──►  (control channel)
```

Both iperf3 server containers are stopped at startup. The proxy starts them automatically on
the first connection (lazy start), which is reflected in the initial connection latency.

## Prerequisites

- Docker and Docker Compose v2
- Linux host (the test scripts use `--network host`; see [Mac/Windows](#macwindows) below)

## Quick start

```sh
# 1. Start the stack (proxy + iperf3 servers — servers start stopped)
cd example/load-test
docker compose up -d

# 2. Run the TCP load test (30 s, 10 parallel streams)
./run-tcp-load-test.sh

# 3. Run the UDP load test (30 s, 10 parallel streams, 100 Mbps target)
./run-udp-load-test.sh

# 4. Tear down
docker compose down
```

## Environment variables

Both scripts read the following variables (override on the command line):

| Variable   | Default | Description |
|------------|---------|-------------|
| `DURATION` | `30`    | Test duration in seconds |
| `PARALLEL` | `10`    | Number of parallel streams |
| `BITRATE`  | `100M`  | Target bitrate for UDP test (e.g. `500M`, `1G`) |

```sh
# Example: longer test with more streams
DURATION=60 PARALLEL=20 ./run-tcp-load-test.sh

# Example: UDP at 500 Mbps
DURATION=60 BITRATE=500M ./run-udp-load-test.sh
```

## Expected output

### TCP

```
==> TCP load test: 10 parallel stream(s) for 30s
    connecting to lazy-tcp-proxy on localhost:5201 -> iperf3-tcp:5201

[ ID] Interval           Transfer     Bitrate
...
[SUM]   0.00-30.00  sec  9.85 GBytes  2.82 Gbits/sec                  sender
[SUM]   0.00-30.00  sec  9.85 GBytes  2.82 Gbits/sec                  receiver
```

### UDP

```
==> UDP load test: 10 parallel stream(s) at 100M for 30s
    connecting to lazy-tcp-proxy on localhost:5202 -> iperf3-udp:5201
    (proxy handles both TCP control channel and UDP data on port 5202)

[ ID] Interval           Transfer     Bitrate         Jitter    Lost/Total Datagrams
...
[SUM]   0.00-30.00  sec   357 MBytes   100 Mbits/sec  0.123 ms  0/256700 (0%)  receiver
```

Key fields to watch:
- **Bitrate** — throughput through the proxy
- **Jitter** (UDP) — variation in packet delay
- **Lost/Total** (UDP) — packet loss through the proxy

## Checking proxy status

While the test is running, poll the status endpoint to confirm the iperf3 containers are
running and traffic is flowing:

```sh
watch -n2 'curl -s http://localhost:8080/metrics | python3 -m json.tool'
```

## Proxy logs

```sh
docker compose logs -f lazy-tcp-proxy
```

You will see the proxy start the iperf3 container on first connection and report active
connections during the test.

## Mac/Windows

`docker run --network host` is Linux-only. On Mac or Windows, replace `localhost` with
`host.docker.internal` and remove `--network host`:

```sh
docker run --rm networkstatic/iperf3 -c host.docker.internal -p 5201 -t 30 -P 10
docker run --rm networkstatic/iperf3 -c host.docker.internal -p 5202 -u -b 100M -t 30 -P 10
```

## Port conflicts

The load test uses ports **5201**, **5202**, and **8080**. Stop any local iperf3 servers or
other services on these ports before starting the stack.
