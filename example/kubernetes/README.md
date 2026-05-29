# lazy-tcp-proxy — Kubernetes Example

This example runs lazy-tcp-proxy inside a Kubernetes cluster alongside an nginx Deployment that starts at **zero replicas** and is scaled to 1 automatically when the first HTTP connection arrives. It scales back to 0 after 5 minutes of inactivity.

## What is deployed

| Resource | Namespace | Purpose |
|----------|-----------|---------|
| `lazy-tcp-proxy` Deployment | `lazy-tcp-proxy` | The proxy itself |
| `lazy-tcp-proxy` Service | `lazy-tcp-proxy` | Exposes the metrics endpoint (port 8080) |
| `lazy-tcp-proxy` ServiceAccount + ClusterRole | `lazy-tcp-proxy` | RBAC to list/watch/scale Deployments |
| `example-app` Deployment (nginx) | `default` | Target service, starts at 0 replicas |
| `example-app` Service | `default` | Lets the proxy reach nginx by DNS |

## Prerequisites

- A running Kubernetes cluster (local: [kind](https://kind.sigs.k8s.io/), [minikube](https://minikube.sigs.k8s.io/), or [k3d](https://k3d.io/))
- `kubectl` configured to talk to that cluster
- The `lazy-tcp-proxy` image available to the cluster

## Start the example

Apply the manifests in order:

```bash
cd example/kubernetes

# 1. Create the proxy namespace
kubectl create namespace lazy-tcp-proxy

# 2. Apply RBAC (ServiceAccount, ClusterRole, ClusterRoleBinding)
kubectl apply -f rbac.yaml

# 3. Deploy the proxy
kubectl apply -f proxy.yaml

# 4. Deploy the example app (starts at 0 replicas)
kubectl apply -f example-app.yaml
```

Wait for the proxy to become ready:

```bash
kubectl rollout status deployment/lazy-tcp-proxy -n lazy-tcp-proxy
```

## Forward ports to localhost

The proxy listens on port 9000 (for example-app) and 8080 (status). Forward both to localhost:

```bash
kubectl port-forward -n lazy-tcp-proxy deploy/lazy-tcp-proxy 8080:8080 9000:9000
```

Leave this running in a terminal. Open a second terminal for the commands below.

Check the status — `example-app` should show `running: false` and 0 replicas:

```bash
curl -s http://localhost:8080/metrics | python3 -m json.tool
```

Also confirm the example-app Deployment is at zero replicas:

```bash
kubectl get deployment example-app -n default
# READY should be 0/0
```

## Trigger on-demand scaling (0 → 1)

Send any HTTP request through the proxy on port 9000:

```bash
curl http://localhost:9000
```

The proxy detects the connection, scales `example-app` from 0 → 1, waits for the pod to become ready, then forwards the request.

## What to look for

**Proxy logs** — watch the scale-up happen in real time:
```bash
kubectl logs -n lazy-tcp-proxy deploy/lazy-tcp-proxy -f
```

You will see lines like:
```
proxy: new connection to example-app (port 80) from ...
k8s: scaling up deployment example-app
proxy: attempt 1: dial example-app.default.svc.cluster.local:80 failed: ...
proxy: attempt 2: dial example-app.default.svc.cluster.local:80 failed: ...
proxy: proxying connection to example-app.default.svc.cluster.local:80
```

**Watch replicas change:**
```bash
kubectl get deployment example-app -n default -w
# READY changes from 0/0 → 1/1 after the first request
```

**Status endpoint** — poll to observe running state:
```bash
watch -n2 'curl -s http://localhost:8080/metrics | python3 -m json.tool'
```

The `running` field flips from `false` → `true` once the pod is ready. After 5 minutes of no requests it flips back to `false` and the Deployment scales to 0.

**Watch idle scale-down:**
```bash
kubectl get deployment example-app -n default -w
# After 5 minutes of inactivity, READY returns to 0/0
```

## Shutdown and cleanup

```bash
# Stop the port-forward (Ctrl+C in that terminal)

# Remove example app
kubectl delete -f example-app.yaml

# Remove the proxy and its namespace
kubectl delete -f proxy.yaml
kubectl delete -f rbac.yaml
kubectl delete namespace lazy-tcp-proxy
```
