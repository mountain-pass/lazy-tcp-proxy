# Document TLS and API Key Options in README Files

**Date Added**: 2026-05-18
**Priority**: Medium
**Status**: Completed

## Problem Statement

The `tls` and `api_key` configuration options (implemented in REQ-067 and renamed in REQ-068) are
not documented in `README_LABELS.md` or `README_CONFIG.md`. Users cannot discover these features
from the reference documentation.

## Functional Requirements

1. Add `lazy-tcp-proxy.tls` and `lazy-tcp-proxy.api-key` rows to the label reference table in `README_LABELS.md`.
2. Add a **TLS Termination** section explaining that `tls: true` wraps the listener with TLS using a shared self-signed certificate and that the feature works for any TCP protocol (not just HTTP).
3. Add an **API Key Authentication** section explaining the `X-API-Key` requirement, 401 behaviour, header stripping before forwarding, keep-alive support, and combined TLS + API key usage.
4. Add `tls` and `api_key` fields to the YAML schema block in `README_CONFIG.md`.
5. Add `tls` and `api_key` to the commented-out placeholder YAML example in `README_CONFIG.md`.

## Success Criteria

- Both labels appear in the label reference table.
- Both fields appear in the YAML schema block.
- The placeholder YAML in `README_CONFIG.md` includes `tls` and `api_key`.
- Documentation is accurate, consistent with the implementation, and linked from the label table.
