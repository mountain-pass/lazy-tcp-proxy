# lazy-tcp-proxy — Unraid

Unraid runs the standard Docker daemon, so lazy-tcp-proxy works on Unraid without any special configuration. The Docker socket is available at the default path and no environment variable overrides are needed.

## Prerequisites

- Unraid 6.x or later with Docker enabled (Settings → Docker → Enable Docker: Yes)

## Method 1: Add Container via the Unraid UI

1. In the Unraid web UI, go to **Docker** → **Add Container**.

2. Fill in the fields:

   | Field | Value |
   |-------|-------|
   | Name | `lazy-tcp-proxy` |
   | Repository | `mountainpass/lazy-tcp-proxy` |
   | Network Type | `bridge` |
   | Console shell command | `sh` |
   | Restart | `Always` |

3. Add a **Path** mapping (click "Add another Path, Port, Variable, Label or Device"):

   | Field | Value |
   |-------|-------|
   | Config Type | Path |
   | Name | Docker socket |
   | Container Path | `/var/run/docker.sock` |
   | Host Path | `/var/run/docker.sock` |
   | Access Mode | Read/Write |

4. Add a **Port** mapping for the status endpoint:

   | Field | Value |
   |-------|-------|
   | Config Type | Port |
   | Name | Status |
   | Container Port | `8080` |
   | Host Port | `8080` |
   | Protocol | TCP |

5. Add **Port** mappings for each service you want proxied (repeat as needed):

   | Field | Value |
   |-------|-------|
   | Config Type | Port |
   | Name | Service port |
   | Container Port | `9001` |
   | Host Port | `9001` |
   | Protocol | TCP |

6. Add **Variable** entries for environment configuration:

   | Config Type | Name | Value |
   |-------------|------|-------|
   | Variable | `IDLE_TIMEOUT_SECS` | `120` |
   | Variable | `POLL_INTERVAL_SECS` | `15` |

7. Click **Apply**.

## Method 2: Community Applications

A Community Applications template is not yet published. Use Method 1 above in the meantime.

## Labelling your containers

In Unraid, Docker labels are set via the **Extra Parameters** field on the container's edit page.

Add a label for each container you want lazy-tcp-proxy to manage:

```
--label "lazy-tcp-proxy.enabled=true" --label "lazy-tcp-proxy.ports=9001:80"
```

To set this on an existing Unraid container:

1. Go to **Docker** → click the container icon → **Edit**.
2. Scroll to the bottom and expand **Extra Parameters**.
3. Paste the `--label` flags into the Extra Parameters field.
4. Click **Apply**.

**Example Extra Parameters for a Gitea container proxied on port 9001:**

```
--label "lazy-tcp-proxy.enabled=true" --label "lazy-tcp-proxy.ports=9001:3000"
```

## Check status

Once lazy-tcp-proxy is running, open a browser or run:

```bash
curl http://<unraid-ip>:8080/metrics
```

All managed containers that are currently stopped will show `"running": false`. They will start automatically when a connection arrives on their proxied port.

## Notes

- Unraid's Docker containers all share the host's Docker daemon. lazy-tcp-proxy can see and manage any container on the system that has the `lazy-tcp-proxy.enabled=true` label.
- Unraid handles Docker networking differently from a standard Linux host — containers use `br0` (bridged) or `host` networking. The proxy joins the target container's Docker network automatically to reach it by internal IP.
- If you use Unraid's **macvlan** networking (containers assigned their own IPs), ensure lazy-tcp-proxy is on the same network or use host networking for the proxy.

For full label and environment variable reference, see the [main README](../../README.md).
