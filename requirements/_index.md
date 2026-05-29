# Requirements Index

| ID      | Title                                      | Priority | Status    | Date Added | File                                                                           |
| ------- | ------------------------------------------ | -------- | --------- | ---------- | ------------------------------------------------------------------------------ |
| REQ-001 | Core TCP Proxy for Docker Containers       | High     | Completed | 2026-03-30 | [2026-03-30-core-tcp-proxy.md](2026-03-30-core-tcp-proxy.md)                   |
| REQ-002 | DOCKER_SOCK Env Var & Dockerfile Volume    | Medium   | Completed | 2026-03-30 | [2026-03-30-docker-sock-env-var.md](2026-03-30-docker-sock-env-var.md)         |
| REQ-003 | Requirements-First Development Workflow    | High     | Completed | 2026-03-30 | [2026-03-30-requirements-workflow.md](2026-03-30-requirements-workflow.md)     |
| REQ-004 | Structured Init and Change Logging         | Medium   | Completed | 2026-03-30 | [2026-03-30-structured-init-and-change-logging.md](2026-03-30-structured-init-and-change-logging.md) |
| REQ-005 | Log All Container Starts with Rejection Reason | High | Completed | 2026-03-30 | [2026-03-30-log-container-start-rejection.md](2026-03-30-log-container-start-rejection.md) |
| REQ-006 | Rename tpc → tcp Throughout                | High     | Completed | 2026-03-30 | [2026-03-30-rename-tpc-to-tcp.md](2026-03-30-rename-tpc-to-tcp.md) |
| REQ-007 | Multi-Port Mappings (ports label)          | High     | Completed | 2026-03-30 | [2026-03-30-multi-port-mappings.md](2026-03-30-multi-port-mappings.md) |
| REQ-008 | Keep Stopped Containers Registered         | High     | Completed | 2026-03-30 | [2026-03-30-keep-stopped-containers-registered.md](2026-03-30-keep-stopped-containers-registered.md) |
| REQ-009 | Fix Container Idle Timeout                 | High     | Completed | 2026-03-30 | [2026-03-30-fix-container-idle-timeout.md](2026-03-30-fix-container-idle-timeout.md) |
| REQ-010 | Idle-Timeout Observability & Poll Interval | Medium   | Completed | 2026-03-30 | [2026-03-30-idle-timeout-observability.md](2026-03-30-idle-timeout-observability.md) |
| REQ-011 | Fix Bidirectional TCP Proxy Teardown       | High     | Completed | 2026-03-30 | [2026-03-30-fix-proxy-teardown.md](2026-03-30-fix-proxy-teardown.md) |
| REQ-012 | Fix Redundant Container Stop Calls         | High     | Completed | 2026-03-30 | [2026-03-30-fix-redundant-stop.md](2026-03-30-fix-redundant-stop.md) |
| REQ-013 | Configurable Idle Timeout (IDLE_TIMEOUT_SECS) | Medium | Completed | 2026-03-30 | [2026-03-30-configurable-idle-timeout.md](2026-03-30-configurable-idle-timeout.md) |
| REQ-014 | Yellow Container Names in Log Output          | Low    | Completed | 2026-03-31 | [2026-03-31-yellow-container-names.md](2026-03-31-yellow-container-names.md) |
| REQ-015 | Container Name in Start/Stop Log Messages     | Low    | Completed | 2026-03-31 | [2026-03-31-container-name-in-start-stop-logs.md](2026-03-31-container-name-in-start-stop-logs.md) |
| REQ-016 | Green Network Names in Log Output             | Low    | Completed | 2026-03-31 | [2026-03-31-green-network-names.md](2026-03-31-green-network-names.md) |
| REQ-017 | Leave Joined Networks on Shutdown             | Medium | Completed | 2026-03-31 | [2026-03-31-leave-networks-on-shutdown.md](2026-03-31-leave-networks-on-shutdown.md) |
| REQ-018 | Reduce Proxy Memory via Buffer Pooling & Idle GC | Medium | Completed | 2026-03-31 | [2026-03-31-reduce-proxy-memory.md](2026-03-31-reduce-proxy-memory.md) |
| REQ-019 | Fix Dependabot Security Alerts (docker + otel)   | High   | Completed   | 2026-03-31 | [2026-03-31-fix-dependabot-security-alerts.md](2026-03-31-fix-dependabot-security-alerts.md) |
| REQ-020 | Fix CVE-2025-54410: Upgrade docker/docker to v28  | High   | Completed   | 2026-03-31 | [2026-03-31-fix-docker-cve-2025-54410.md](2026-03-31-fix-docker-cve-2025-54410.md) |
| REQ-021 | Cyan Source IP Address in Connection Logs         | Low    | Completed   | 2026-03-31 | [2026-03-31-cyan-source-ip.md](2026-03-31-cyan-source-ip.md) |
| REQ-022 | Per-Service Allow-List and Block-List via Labels  | Medium | Completed   | 2026-03-31 | [2026-03-31-allow-block-lists.md](2026-03-31-allow-block-lists.md) |
| REQ-023 | Discovered/Registered Containers Start as Idle    | High   | Completed   | 2026-04-01 | [2026-04-01-discovered-containers-start-idle.md](2026-04-01-discovered-containers-start-idle.md) |
| REQ-024 | Handle Port Conflicts Between Containers          | High   | Completed   | 2026-04-01 | [2026-04-01-handle-port-conflicts.md](2026-04-01-handle-port-conflicts.md) |
| REQ-025 | HTTP Status Endpoint (List Managed Containers)    | High   | Completed   | 2026-04-01 | [2026-04-01-http-status-endpoint.md](2026-04-01-http-status-endpoint.md) |
| REQ-026 | Webhook Support for Container Lifecycle Events    | Medium | Completed   | 2026-04-01 | [2026-04-01-webhook-support.md](2026-04-01-webhook-support.md) |
| REQ-027 | UDP Traffic Support                               | Medium | Completed   | 2026-04-01 | [2026-04-01-udp-traffic-support.md](2026-04-01-udp-traffic-support.md) |
| REQ-028 | Integration Tests (TCP and UDP Proxy)             | Medium | Completed   | 2026-04-01 | [2026-04-01-integration-tests.md](2026-04-01-integration-tests.md) |
| REQ-029 | Root Redirect to /status                          | Low    | Completed ⚠️ superseded by REQ-056 (Status Dashboard) | 2026-04-02 | [2026-04-02-root-redirect-to-status.md](2026-04-02-root-redirect-to-status.md) |
| REQ-030 | Last Active Default & Relative Time Field         | Medium | Completed   | 2026-04-02 | [2026-04-02-last-active-relative.md](2026-04-02-last-active-relative.md) |
| REQ-031 | GitHub Actions Go CI Workflow                     | High   | Completed   | 2026-04-02 | [2026-04-02-github-actions-go-ci.md](2026-04-02-github-actions-go-ci.md) |
| REQ-032 | Fix golangci-lint errcheck Violations             | High   | Completed   | 2026-04-02 | [2026-04-02-fix-lint-errcheck.md](2026-04-02-fix-lint-errcheck.md) |
| REQ-033 | Fix Second Wave of golangci-lint Violations       | High   | Completed   | 2026-04-02 | [2026-04-02-fix-lint-wave2.md](2026-04-02-fix-lint-wave2.md) |
| REQ-034 | Fix govet hostport IPv6 Violation                 | High   | Completed   | 2026-04-02 | [2026-04-02-fix-lint-wave3.md](2026-04-02-fix-lint-wave3.md) |
| REQ-035 | Migrate docker/docker → moby/moby/client          | High   | Completed   | 2026-04-03 | [2026-04-03-migrate-docker-client-module.md](2026-04-03-migrate-docker-client-module.md) |
| REQ-036 | UDP Test Container in Example Docker Compose      | Low    | Completed   | 2026-04-03 | [2026-04-03-udp-test-container.md](2026-04-03-udp-test-container.md) |
| REQ-037 | Per-Container Idle Timeout Label Override         | Medium | Completed   | 2026-04-03 | [2026-04-03-idle-timeout-label-override.md](2026-04-03-idle-timeout-label-override.md) |
| REQ-038 | Kubernetes Backend (BACKEND=kubernetes)           | High   | Completed ⚠️ superseded by REQ-049 (`BACKEND` env var replaced by image-based selection) | 2026-04-04 | [2026-04-04-kubernetes-backend.md](2026-04-04-kubernetes-backend.md) |
| REQ-039 | Reorganise Example Directory (docker/ subdir)     | Low    | Completed   | 2026-04-04 | [2026-04-04-reorganise-example-dir.md](2026-04-04-reorganise-example-dir.md) |
| REQ-040 | Example README Files (Docker and Kubernetes)      | Low    | Completed   | 2026-04-05 | [2026-04-05-example-readmes.md](2026-04-05-example-readmes.md) |
| REQ-041 | Webhook Connection Events (connection_started/ended) | Medium | Completed   | 2026-04-07 | [2026-04-07-webhook-connection-events.md](2026-04-07-webhook-connection-events.md) |
| REQ-042 | Sort /status Services by Name                     | Low    | Completed   | 2026-04-07 | [2026-04-07-sort-status-services-by-name.md](2026-04-07-sort-status-services-by-name.md) |
| REQ-043 | UDP-Only Config Validation Fix                    | High   | Completed   | 2026-04-07 | [2026-04-07-udp-only-config-validation.md](2026-04-07-udp-only-config-validation.md) |
| REQ-044 | Webhook Connection Events — Add Source IP Address | Medium | Completed   | 2026-04-07 | [2026-04-07-webhook-connection-ip-address.md](2026-04-07-webhook-connection-ip-address.md) |
| REQ-045 | Dependency Cascade (lazy-tcp-proxy.dependants)    | Medium | Completed   | 2026-04-07 | [2026-04-07-docker-dependency-cascade.md](2026-04-07-docker-dependency-cascade.md) |
| REQ-046 | UDP Flow Webhook Events & Rename TCP Event Names  | Medium | Completed   | 2026-04-07 | [2026-04-07-webhook-udp-flow-events.md](2026-04-07-webhook-udp-flow-events.md) |
| REQ-047 | Fix Slow Cross-Platform Docker Build (QEMU → Native Cross-Compilation) | Medium | Completed | 2026-04-07 | [2026-04-07-fix-slow-cross-platform-docker-build.md](2026-04-07-fix-slow-cross-platform-docker-build.md) |
| REQ-048 | Cron-Based Scheduling (Docker & Kubernetes)       | Medium | Completed   | 2026-04-07 | [2026-04-06-cron-scheduling.md](2026-04-06-cron-scheduling.md) |
| REQ-049 | Separate Kubernetes Build Artifact (mountainpass/lazy-tcp-proxy-k8s)   | Medium | Completed   | 2026-04-07 | [2026-04-07-separate-k8s-build-artifact.md](2026-04-07-separate-k8s-build-artifact.md) |
| REQ-050 | Singleflight Deduplication for Container Startup | Medium | Completed   | 2026-04-07 | [2026-04-07-singleflight-container-startup.md](2026-04-07-singleflight-container-startup.md) |
| REQ-051 | Fix Kubernetes WatchEvents Gaps                  | Medium | Completed   | 2026-04-07 | [2026-04-07-fix-k8s-watchevents.md](2026-04-07-fix-k8s-watchevents.md) |
| REQ-052 | Load Tests (TCP and UDP Proxy)                                          | Medium | Completed   | 2026-04-07 | [2026-04-07-load-tests-tcp-udp.md](2026-04-07-load-tests-tcp-udp.md) |
| REQ-053 | Platform Integration Documentation (Podman, Unraid, TrueNAS SCALE)     | Medium | Completed   | 2026-04-08 | [2026-04-08-platform-integration-docs.md](2026-04-08-platform-integration-docs.md) |
| REQ-054 | Docker Compose Recipes for Popular Service Images                       | Medium | Completed   | 2026-04-08 | [2026-04-08-docker-recipes-popular-services.md](2026-04-08-docker-recipes-popular-services.md) |
| REQ-055 | Fix UDP First Packet Drop on Container Startup                          | High   | Completed   | 2026-04-09 | [2026-04-09-fix-udp-first-packet-drop.md](2026-04-09-fix-udp-first-packet-drop.md) |
| REQ-056 | Status Dashboard (HTML UI at /)                                         | Low    | Completed   | 2026-04-14 | [2026-04-14-status-dashboard.md](2026-04-14-status-dashboard.md) |
| REQ-057 | Fix UDP ECONNREFUSED Retry and Clarify Internal Flow Log Messages        | High   | Completed   | 2026-04-14 | [2026-04-14-fix-udp-connrefused-retry-and-flow-logs.md](2026-04-14-fix-udp-connrefused-retry-and-flow-logs.md) |
| REQ-058 | Fix: Stale Docker Network Error on Container Start                      | High   | Completed   | 2026-04-11 | [2026-04-11-fix-stale-network-error.md](2026-04-11-fix-stale-network-error.md) |
| REQ-059 | Add `name:` Field to All Recipe Compose Files                           | High   | Completed   | 2026-04-11 | [2026-04-11-recipe-compose-name-field.md](2026-04-11-recipe-compose-name-field.md) |
| REQ-060 | Fix CVE-2026-32283: Upgrade Go Base Image to 1.25.9                     | High   | Completed   | 2026-04-14 | [2026-04-14-fix-cve-2026-32283-golang-1.25.9.md](2026-04-14-fix-cve-2026-32283-golang-1.25.9.md) |
| REQ-061 | Fix UDP Cold-Start Timeouts for Slow-Starting Upstreams (e.g. Pi-hole)  | High   | Completed   | 2026-04-14 | [2026-04-14-fix-udp-pihole-cold-start-timeouts.md](2026-04-14-fix-udp-pihole-cold-start-timeouts.md) |
| REQ-062 | Wire START_TIMEOUT_SECS to TCP Dial Retry Loop                           | Medium | Completed   | 2026-04-14 | [2026-04-14-wire-start-timeout-tcp.md](2026-04-14-wire-start-timeout-tcp.md) |
| REQ-063 | HTTP Health Check Label for Container Readiness                          | Medium | Completed   | 2026-04-14 | [2026-04-14-http-health-check.md](2026-04-14-http-health-check.md) |
| REQ-064 | Docker HEALTHCHECK Readiness Gate                                        | Medium | Completed   | 2026-04-15 | [2026-04-15-docker-healthcheck-readiness.md](2026-04-15-docker-healthcheck-readiness.md) |
| REQ-065 | Dynamic Configuration File (YAML Override Store)                         | High   | Completed   | 2026-05-13 | [2026-05-13-dynamic-config-file.md](2026-05-13-dynamic-config-file.md) |
| REQ-066 | Config Placeholder — Commented Example                                   | Low    | Completed   | 2026-05-15 | [2026-05-15-config-placeholder-example.md](2026-05-15-config-placeholder-example.md) |
| REQ-067 | Per-Service TLS Termination and API Key Authentication                   | High   | Completed   | 2026-05-15 | [2026-05-15-per-service-tls-and-api-key.md](2026-05-15-per-service-tls-and-api-key.md) |
| REQ-068 | Docker Stack Service Scaling                                             | High   | Completed   | 2026-05-15 | [2026-05-15-docker-stack-scaling.md](2026-05-15-docker-stack-scaling.md) |
| REQ-069 | Traefik Integration (HTTP Provider Endpoint)                             | High   | Completed   | 2026-05-18 | [2026-05-18-traefik-integration.md](2026-05-18-traefik-integration.md) |
| REQ-070 | Document TLS and API Key Options in README Files                         | Medium | Completed   | 2026-05-18 | [2026-05-18-document-tls-apikey.md](2026-05-18-document-tls-apikey.md) |
| REQ-071 | GitHub Actions Docker Hub Publish Workflow                               | High   | Completed   | 2026-05-18 | [2026-05-18-github-action-docker-publish.md](2026-05-18-github-action-docker-publish.md) |
| REQ-072 | Fix Go CI Build (Unkeyed PortMapping Struct Literals)                    | High   | Completed   | 2026-05-18 | [2026-05-18-fix-go-ci-build.md](2026-05-18-fix-go-ci-build.md) |
| REQ-073 | Fix golangci-lint QF1008 Violations in docker/manager.go                 | High   | Completed   | 2026-05-18 | [2026-05-18-fix-go-ci-lint-qf1008.md](2026-05-18-fix-go-ci-lint-qf1008.md) |
| REQ-074 | Fix CVE: Upgrade go.opentelemetry.io/otel to 1.41.0                     | High   | Completed   | 2026-05-18 | [2026-05-18-fix-otel-cve-1.41.0.md](2026-05-18-fix-otel-cve-1.41.0.md) |
| REQ-075 | Traefik Entrypoint and CertResolver Configuration                        | High   | Completed   | 2026-05-18 | [2026-05-18-traefik-entrypoint-certresolver.md](2026-05-18-traefik-entrypoint-certresolver.md) |
| REQ-076 | Traefik Default Environment Variable Values                              | Low    | Completed     | 2026-05-18 | [2026-05-18-traefik-default-env-vars.md](2026-05-18-traefik-default-env-vars.md) |
| REQ-077 | Auto-Join Docker Networks for Static Config Targets                      | High   | Completed     | 2026-05-18 | [2026-05-18-join-networks-for-config-targets.md](2026-05-18-join-networks-for-config-targets.md) |
| REQ-078 | Basic Auth Support and Multi-Value API Key                               | High   | Completed     | 2026-05-18 | [2026-05-18-basic-auth-and-multi-api-key.md](2026-05-18-basic-auth-and-multi-api-key.md) |
| REQ-079 | Fix: Config-Only Services Always Show as Stopped                         | High   | Completed     | 2026-05-18 | [2026-05-18-fix-managed-services-display.md](2026-05-18-fix-managed-services-display.md) |
| REQ-080 | Fix: Config-Only Container Disappears After docker compose up            | High   | Completed     | 2026-05-18 | [2026-05-18-fix-config-only-recreate.md](2026-05-18-fix-config-only-recreate.md) |
| REQ-081 | Traefik TCP and UDP Provider Sections                                    | Medium | Completed ⚠️ superseded by REQ-082 (Traefik TCP SNI via traefik_tcp_hosts) | 2026-05-24 | [2026-05-24-traefik-tcp-udp-sections.md](2026-05-24-traefik-tcp-udp-sections.md) |
| REQ-082 | Traefik TCP SNI Routing via `traefik_tcp_hosts`                          | Medium | Completed     | 2026-05-24 | [2026-05-24-traefik-tcp-sni-hosts.md](2026-05-24-traefik-tcp-sni-hosts.md) |
| REQ-083 | Container Availability Config (`availability`)                           | Medium | Completed     | 2026-05-27 | [2026-05-27-container-availability-config.md](2026-05-27-container-availability-config.md) |
| REQ-084 | Status Dashboard — Cards to Table Layout                                 | Low    | Completed     | 2026-05-27 | [2026-05-27-cards-to-table-layout.md](2026-05-27-cards-to-table-layout.md) |
| REQ-085 | WEB_PORT and WEB_HOST Environment Variables                              | Medium | Completed     | 2026-05-28 | [2026-05-28-web-port-and-web-host.md](2026-05-28-web-port-and-web-host.md) |
| REQ-086 | Status Dashboard — ⚠️ Icon for Missing/Removed Containers               | Medium | Completed     | 2026-05-29 | [2026-05-29-missing-container-warning-icon.md](2026-05-29-missing-container-warning-icon.md) |
| REQ-087 | README Warning: `docker system prune` Removes Stopped Containers         | Medium | Completed     | 2026-05-29 | [2026-05-29-readme-docker-system-prune-warning.md](2026-05-29-readme-docker-system-prune-warning.md) |
