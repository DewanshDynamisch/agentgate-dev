---
title: Governance API Reference
sidebar_position: 3
description: Exact endpoints, request/response shapes, and auth of the admin governance REST API.
---

# Governance API Reference

Implemented in `internal/govapi`
(`handler.go`, `types.go`, `middleware.go`), registered by
`govapi.RegisterRoutes`. Every route requires the admin key; unauthenticated or
wrong-key requests return `401` with the standard `ErrorResponse` shape.

## Authentication

Send the admin key (`AGENTGATE_ADMIN_TOKEN`) in **either** header:

```text
X-AgentGate-Admin-Key: agentgate-admin-secret-dev
# or
Authorization: Bearer agentgate-admin-secret-dev
```

Compared **constant-time** (`crypto/subtle`). A request with no key or a wrong
key:

```json
HTTP/1.1 401
{ "error": { "code": "UNAUTHORIZED", "message": "..." } }
```

## Routes

### `POST /api/v1/workspaces/{workspace_id}/policies` — create candidate

```json
{ "content": "permit(principal, action, resource);", "description": "permit all (dev)" }
```

`201 Created` → the stored `PolicyRecord` (workspace_id, version = SHA-256 of
content, content, state, description, created_at). Content-addressable: re-creating
the same content returns the existing version.

### `POST /api/v1/workspaces/{workspace_id}/policies/validate` — syntax check (no mutation)

```json
{ "content": "forbid(principal, action, resource);" }
```

- Valid → `200`: `{ "valid": true, "version": "<sha256>" }`
- Invalid → `200`: `{ "valid": false, "errors": ["…"] }` (valid+errors never coexist)

Errors are validation results, not transport failures — hence `200`.

### `POST /api/v1/workspaces/{workspace_id}/policies/{version}/activate` — atomic activate

No body. `200`:

```json
{
  "workspace_id": "workspace-alpha",
  "active_version": "4f3a…",
  "previous_version": "2f7c…",
  "activated_at": "2026-09-14T09:00:00Z"
}
```

Activating an unknown version → `404`. The swap is atomic (store transaction +
in-memory engine pointer); a failure leaves the last-known-good active.

### `POST /api/v1/workspaces/{workspace_id}/policies/rollback` — roll back

```json
{ "target_version": "2f7c…" }
```

`200`: `{ "workspace_id":…, "active_version": "2f7c…", "rolled_back_from": "4f3a…", "activated_at": "…" }`
Rollback to a non-existent version → `404` and the active policy is unchanged.

### `GET /api/v1/workspaces/{workspace_id}/policies` — list versions

`200`: `{ "workspace_id": "workspace-alpha", "policies": [ PolicyRecord… ] }`
(one entry per version/state, newest first).

### `GET /api/v1/workspaces/{workspace_id}/policies/{version}` — fetch version

`200` → `PolicyRecord`. Unknown version → `404`.

### `POST /api/v1/workspaces/{workspace_id}/policies/{version}/preview` — evaluate samples, no mutation

```json
{
  "sample_requests": [
    { "principal_id": "agent-7", "principal_roles": ["payer"], "resource_id": "stripe/charge", "resource_risk": "write" }
  ]
}
```

`200`:

```json
{
  "version": "4f3a…",
  "results": [ { "allowed": true, "matched": true, "had_error": false } ]
}
```

### `POST /api/v1/workspaces/{workspace_id}/policies/{version}/dryrun` — active vs candidate (G4)

```json
{
  "sample_requests": [
    {
      "execution_id": "exec-0001",
      "principal_id": "agent-7",
      "principal_roles": ["payer"],
      "on_behalf_of": "user-12",
      "backend_id": "stripe",
      "tool_name": "charge",
      "risk": "write"
    }
  ]
}
```

`200`:

```json
{
  "candidate_version": "4f3a…",
  "results": [
    {
      "active_decision": "DENY",   "active_reason": "policy_deny",   "active_policy_version": "2f7c…",
      "candidate_decision": "ALLOW","candidate_reason": "policy_allow","candidate_policy_version": "4f3a…",
      "changed": true
    }
  ]
}
```

`changed` is true when decision **or** reason differs. The active engine is never
touched — dry-run is read-only. If the handler was constructed without the
governance integration service this endpoint is unavailable (`nil` guard).

## Error shape (all routes)

```json
{ "error": { "code": "BAD_REQUEST" | "UNAUTHORIZED" | "NOT_FOUND" | "CONFLICT" | "INTERNAL", "message": "…" } }
```

## PolicyRecord

```json
{
  "workspace_id": "workspace-alpha",
  "version": "4f3a…",
  "content": "permit(principal, action, resource);",
  "state": "candidate" | "active" | "historical",
  "description": "…",
  "created_at": "…",
  "activated_at": null
}
```

The TypeScript mirror of every shape above lives in
`frontend/src/models/governance.ts` (kept in sync with these JSON bodies).