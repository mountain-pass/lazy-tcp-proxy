# Fix: Compose Re-provision — Wait for Container Running After Up()

**Date Added**: 2026-05-29
**Priority**: High
**Status**: Completed

## Problem Statement

`docker compose Up()` creates the container but sometimes returns an error
("no container found for project") before it finishes starting the container.
This leaves the container in `"Created"` state.  The proxy logs
"could not start container" and drops the connection, even though the container
was successfully provisioned and just needs `docker start`.

Root cause (observed in logs): compose's internal project lookup races against the
container it just created, returning an error from `Start()` immediately after `Create()`
succeeds.

## Functional Requirements

1. After `compose Up()` returns (error or not), poll `ContainerInspect` with a 1-second
   interval until the container appears.
2. If the container is found in any non-running state (`"created"`, `"exited"`, etc.),
   call `ContainerStart` directly.
3. If the container is already running, return success immediately.
4. If the container does not appear within 30 seconds, return the original `Up()` error
   (or a timeout error if `Up()` succeeded but the container never appeared).
5. The `"re-provisioned successfully"` log line is only emitted after the container is
   confirmed running.

## Acceptance Criteria

- [ ] A connection arriving when the container is missing triggers compose up, and the
      first connection succeeds — no second connection required.
- [ ] The proxy log shows the container starting after compose up when `Up()` errors
      partway through.
- [ ] If compose completely fails (no container created), the proxy returns an error
      after the 30-second poll timeout.

## Dependencies

- Fixes REQ-088 (Compose Re-provision on Missing Container).
