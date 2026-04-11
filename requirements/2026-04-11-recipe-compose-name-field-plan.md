# Add `name:` Field to All Recipe Compose Files — Implementation Plan

**Requirement**: [2026-04-11-recipe-compose-name-field.md](2026-04-11-recipe-compose-name-field.md)
**Date**: 2026-04-11
**Status**: Implemented

## Implementation Steps

1. Prepend `name: <slug>\n\n` to each recipe file. The slug is the segment of the
   filename between `docker-compose.` and the port suffix (`.NNN.yml`).

2. Update REQ-057 status to Completed in the requirement file and index.

## File Change Summary

| File | Name value |
|------|------------|
| `recipes/docker-compose.jellyfin.9000.yml` | `jellyfin` |
| `recipes/docker-compose.kafka.9092.yml` | `kafka` |
| `recipes/docker-compose.lazy-tcp-proxy.yml` | `lazy-tcp-proxy` |
| `recipes/docker-compose.mariadb.3306.yml` | `mariadb` |
| `recipes/docker-compose.memcached.11211.yml` | `memcached` |
| `recipes/docker-compose.minecraft.25565.yml` | `minecraft` |
| `recipes/docker-compose.minio.9000,9001.yml` | `minio` |
| `recipes/docker-compose.mongo.27017.yml` | `mongo` |
| `recipes/docker-compose.mysql.3306.yml` | `mysql` |
| `recipes/docker-compose.n8n.5678.yml` | `n8n` |
| `recipes/docker-compose.nginx.9006.yml` | `nginx` |
| `recipes/docker-compose.ollama-cpu.9001-9002.yml` | `ollama-cpu` |
| `recipes/docker-compose.ollama-gpu.9001-9002.yml` | `ollama-gpu` |
| `recipes/docker-compose.open-ssh.9004.yml` | `open-ssh` |
| `recipes/docker-compose.pihole.53,9006.yml` | `pihole` |
| `recipes/docker-compose.plex.32400.yml` | `plex` |
| `recipes/docker-compose.postgres.5432.yml` | `postgres` |
| `recipes/docker-compose.rabbitmq.5672,15672.yml` | `rabbitmq` |
| `recipes/docker-compose.redis.6379.yml` | `redis` |
| `recipes/docker-compose.registry.5000.yml` | `registry` |
| `recipes/docker-compose.samba.445.yml` | `samba` |
| `recipes/docker-compose.selenium-chromium.4444,7900.yml` | `selenium-chromium` |
| `recipes/docker-compose.udp-echo.9005.yml` | `udp-echo` |
| `recipes/docker-compose.uptime-kuma.3001.yml` | `uptime-kuma` |
| `recipes/docker-compose.verdaccio.4873.yml` | `verdaccio` |
| `recipes/docker-compose.whoami.9003.yml` | `whoami` |
| `recipes/docker-compose.wordpress.8080.yml` | `wordpress` |

## Risks & Open Questions

None — purely additive change with no effect on running containers.
