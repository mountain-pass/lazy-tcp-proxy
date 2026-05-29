
# Questions and Answers

## Table of contents

- [Is there any way to see what containers are managed?](#is-there-any-way-to-see-what-containers-are-managed)
- [What happens when there aren't enough resources?](#what-happens-when-there-arent-enough-resources)
- [Is there an example setup I can try?](#is-there-an-example-setup-i-can-try)
- [What happens on container port conflicts?](#what-happens-on-container-port-conflicts)
- [Can I run two instances of lazy-tcp-proxy?](#can-i-run-two-instances-of-lazy-tcp-proxy)
- [Does the app handle UDP Traffic?](#does-the-app-handle-udp-traffic)
- [Does the app support calling webhooks?](#does-the-app-support-calling-webhooks)
- [Does the app support Docker Swarm/Stacks?](#does-the-app-support-docker-swarmstacks)
- [Does the app support Kubernetes?](#does-the-app-support-kubernetes)
- [Does the app support starting based on a webhook/cron schedule/message queue?](#does-the-app-support-starting-based-on-a-webhookcron-schedulemessage-queue)
- [Does this app support load balancing?](#does-this-app-support-load-balancing)
- [Does this app support transitive container starting?](#does-this-app-support-transitive-container-starting)



### Is there any way to see what containers are managed?

> Yes! The logs, and the new http://localhost:8080/metrics HTTP endpoint (`WEB_PORT` environment variable).

### What happens when there aren't enough resources?

> The app has no awareness of resource capacity. It will just try to start the requested service.
>
> [Let me know how if it should handle this scenario differently!](../../issues)

### Is there an example setup I can try?

> Yes, please check the example docker compose under [example/docker-compose.yml](example/docker-compose.yml).

### What happens on container port conflicts?

> The app logs an error, and ignores the second conflicting container. 

### Can I run two instances of lazy-tcp-proxy?

> Yes, but it would be redundant. They would both just duplicate control. There is no way to scope the managed containers at this point.

### Does the app handle UDP Traffic?

> Not yet. But it could.
>
> [Let me know if you want this!](../../issues)

### Does the app support calling webhooks?

> Yes! Check the `lazy-tcp-proxy.webhook-url=...` configuration label.

### Does the app support Docker Swarm/Stacks?

> Maybe - I haven't tested this. But it could.
>
> It would be nice if it could also scale >1 if required.
>
> [Let me know if you want this!](../../issues)

### Does the app support Kubernetes?

> Not yet. But it could.
>
> It would be nice if it could also scale >1 if required.
>
> [Let me know if you want this!](../../issues)

### Does the app support starting based on a webhook/cron schedule/message queue?

> Not yet. But it could.
>
> [Let me know if you want this!](../../issues)

### Does this app support load balancing?

> No. But with Docker Swarms we might get this for free.
>
> [Let me know if you want this!](../../issues)

### Does this app support transitive container starting?

> E.g. If this port is accessed, start these two containers.
> 
> Not yet. But it could.
>
> [Let me know if you want this!](../../issues)
