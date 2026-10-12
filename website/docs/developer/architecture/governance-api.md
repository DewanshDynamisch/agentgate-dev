---
title: Governance API
sidebar_position: 7
description: The admin-authenticated REST API that drives the policy lifecycle.
---

# Governance API

The governance API (`internal/govapi`) is the authenticated REST surface for
the policy lifecycle. It is **admin-only** — every
`/api/v1/workspaces/{workspace_id}/…` request must present the admin key.

## Authentication

`AdminAuthMiddleware` (`internal/govapi/middleware.go:11`) compares the shared
admin key using `crypto/subtle.ConstantTimeCompare` (constant-time — no timing
side channel). The key is accepted from either:

- `X-AgentGate-Admin-Key: <token>`, or
- `Authorization: Bearer <token>`.

The dev default value is `agentgate-admin-secret-dev`
(`AGENTGATE_ADMIN_TOKEN` env). Unauthenticated requests get
`401` with `{"error":{"code":"UNAUTHORIZED",…}}`. Production is expected to
harden this surface further (authenticated HTTPS/TLS, network restriction) per
[PRODUCTION-INVARIANTS §7](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/SECURITY/PRODUCTION-INVARIANTS.md).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/workspaces/{id}/policies` | List all versions (newest first). |
| `GET` | `/api/v1/workspaces/{id}/policies/{version}` | Fetch one version (404 if missing). |
| `POST` | `/api/v1/workspaces/{id}/policies` | Create a candidate (`{content, description}`) → 201 + `PolicyRecord`. |
| `POST` | `/api/v1/workspaces/{id}/policies/validate` | Validate Cedar syntax only (`{content}`) → `{valid, version?, errors?}`. |
| `POST` | `/api/v1/workspaces/{id}/policies/{version}/activate` | Atomically activate that version. |
| `POST` | `/api/v1/workspaces/{id}/policies/rollback` | Roll back to `{target_version}`; returns `rolled_back_from`. |
| `POST` | `/api/v1/workspaces/{id}/policies/{version}/preview` | Run sample `EvalInput`s against that version (no mutation). |
| `POST` | `/api/v1/workspaces/{id}/policies/{version}/dryrun` | Active-vs-candidate decision comparison (requires the governance integration layer). |

Routes are registered in `internal/govapi/handler.go:34` and the wire models
(`PolicyRecord`, activation/rollback/preview/dry-run request & response shapes,
and the `ErrorResponse` contract) live in `internal/govapi/types.go`.

## Policy record shape

```text
PolicyRecord {
  workspace_id, version, content, state ("candidate"|"active"|"historical"),
  description, created_at, activated_at?
}
```

Every mutation is an auditable control-plane operation (see
[policy lifecycle](./policy-lifecycle.md) for the mutation-event plumbing).

## Next

- [API reference](../reference/api-reference.md) — exact request/response examples.
- [Governance↔Decision integration](./governance-decision-integration.md).