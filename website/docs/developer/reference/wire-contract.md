---
title: Wire Contract — /evaluate
sidebar_position: 2
description: The frozen JSON request/response contract of the decision core, exposed by the mock and verified by g1blackbox.
---

# Wire Contract — `POST /evaluate`

Frozen at G1. The canonical reference is
[`docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md)
(freeze rule: changing any field requires the checkpoint-defined process, not a
quiet edit). Transport implementation: `internal/mockauthz`
(`agentgate/internal/mockauthz/wire.go`), which wraps the **real**
`decision.Engine` — every response is a genuine Cedar decision, never hand-rolled
mock logic.

## Request

```json
{
  "execution_id": "exec-0001",
  "workspace_id": "workspace-alpha",
  "identity":   { "agent_id": "agent-7", "on_behalf_of": "user-12", "roles": ["payer"] },
  "tool":       { "backend_id": "stripe", "name": "charge" },
  "classification": { "known": true, "risk": "write" },
  "arguments": { "amount": { "type": "int", "value": 700 } }
}
```

Field rules:

- `arguments` values must be `{"type": "string"|"int"|"bool", "value": <matching JSON>}`.
  A value that is a **literal `null`**, has an unknown `type`, or mismatches its
  declared type decodes to an invalid `AttributeValue` → `malformed_request`
  (DENY). This explicit-null check blocks the null→zero-value decision flip
  found independently by three G1 workstreams.
- **Unknown fields are rejected at the transport** — the decoder uses
  `DisallowUnknownFields()`, so an unexpected key is an HTTP 400 *before* any
  decision exists (the transport-level fixture; nothing reaches Cedar).
- `roles`, `on_behalf_of`, `arguments` are optional in JSON shape but their
  empty values fail closed in the domain core.

## Response

All five fields are **always present** — including `message`/`policy_version`
as `""` when empty. Deliberately **not** `omitempty` (a G1 freeze correction).

```json
{
  "decision": "DENY",
  "reason": "policy_deny",
  "message": "…authored by internal/decision, never Cedar-internal text…",
  "policy_version": "2f7c…",
  "execution_id": "exec-0001"
}
```

`execution_id` is echoed unchanged on **every** path, including all failure
paths.

## Deny reasons (the complete deterministic set)

| `reason` | Meaning | Cedar reached? |
|---|---|---|
| `policy_allow` | explicit permit matched | yes |
| `policy_deny` | explicit forbid matched (or permit condition failed as matched-denying) | yes |
| `no_matching_policy` | evaluated, nothing matched — deny by default | yes |
| `invalid_identity` | identity missing/ambiguous/unusable | **no** |
| `unknown_tool` | tool not classified (unconditional, even for admin) | **no** |
| `malformed_request` | structurally incomplete / zero-value attribute | **no** |
| `evaluation_error` | Cedar reported an internal error — never trusted as ALLOW | yes (errored) |
| `no_policy_loaded` | engine has no valid policy | **no** |

## Transport vs. domain errors

- A body that fails to **decode** (malformed JSON, unknown field): HTTP 400,
  no decision object.
- A body that **decodes but is semantically invalid** (missing agent id, etc.):
  HTTP 200 whose body is a DENY `Result` — the decision is data in the
  response, not conveyed by the status code (mirrors a real ext_authz
  boundary). Any non-200 status is a transport problem, not an authorization
  decision.

## Boundaries

- This `.wire` JSON is a **transport-specific shape** for the decision core; the
  frozen domain contract is `decision.Request`/`decision.Result`
  (`internal/decision/types.go`). The two must never be merged — transport
  types stay out of `internal/decision`.
- `Message` must **never** be populated from Cedar's own diagnostic text — it
  is authored only by `internal/decision`.
- O-008: agentgateway's native `ext_authz` cannot construct this body; the
  production mapping is the future `internal/authz` layer (see
  [Gateway integration](../architecture/gateway-integration.md)).