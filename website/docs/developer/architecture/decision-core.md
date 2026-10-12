---
title: Decision Core (Frozen Contract)
sidebar_position: 3
description: The authorization decision core — request/result shape, fail-closed matrix, and the Cedar boundary.
---

# Decision Core (Frozen Contract)

The decision core is the frozen G1 authorization contract. Its canonical,
field-by-field definition lives in
[`docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md);
everything here is a developer-oriented summary.

## Where it lives

| Concern | Location |
|---|---|
| `decision.Request` / `decision.Result` types | `agentgate/internal/decision/types.go` |
| Evaluation flow (fail-closed) | `agentgate/internal/decision/engine.go` |
| Cedar boundary (only Cedar-importing package) | `agentgate/internal/policy` |
| Wire layer for the mock | `agentgate/internal/mockauthz` |
| Canonical fixture policy | `agentgate/internal/fixturepolicy` |

> ### 🔒 Freeze rule
> Changing `decision.Request`, `decision.Result`, or the `mockauthz` wire shape
> requires Architect review plus updated tests in both `internal/decision` and
> `internal/mockauthz`. This is a hard process rule, not a suggestion.

## `decision.Request` (the input)

```go
type Request struct {
    ExecutionID    string            // required, non-empty — echoed on every result path
    WorkspaceID    string            // required — carried for audit/tanancy, not yet evaluated
    Identity       Identity          // AgentID (req), OnBehalfOf (optional), Roles (required, non-empty)
    Tool           ToolRef           // BackendID + Name — "backend/name" becomes the Cedar resource id
    Classification ToolClassification // Known (bool) + Risk ("read"|"write"|"destructive")
    Arguments      map[string]AttributeValue // declared, typed attributes only
}
```

`AttributeValue` is a closed `string`/`int64`/`bool` set built via
`StringAttr`/`IntAttr`/`BoolAttr`; a zero-value or literal JSON `null`
attribute is malformed, never a silent zero.

## `decision.Result` (the output)

```go
type Result struct {
    Decision      Decision    // exactly "ALLOW" or "DENY" — there is no third "error" value
    Reason        ReasonCode  // deterministic enum, see the matrix below
    Message       string      // fixed AgentGate-authored strings; never raw Cedar diagnostics
    PolicyVersion string      // hex SHA-256 of the evaluated Cedar bytes
    ExecutionID   string      // copied unchanged from the request
}
```

## Fail-closed matrix

| Condition | Decision | Reason | PolicyVersion |
|---|---|---|---|
| Explicit Cedar permit matched | ALLOW | `policy_allow` | evaluated version |
| Explicit Cedar forbid matched | DENY | `policy_deny` | evaluated version |
| No Cedar policy matched (default-deny) | DENY | `no_matching_policy` | evaluated version |
| Missing/empty `AgentID` | DENY | `invalid_identity` | `""` (Cedar never reached) |
| `OnBehalfOf == AgentID` (non-empty) | DENY | `invalid_identity` | `""` |
| Empty or blank-entry `Roles` | DENY | `invalid_identity` | `""` |
| `Classification.Known == false` | DENY | `unknown_tool` | `""` |
| `Known == true` and `Risk` empty/blank | DENY | `malformed_request` | `""` |
| Missing `ExecutionID`/`WorkspaceID`/`BackendID`/`Name` | DENY | `malformed_request` | `""` |
| Zero-value/invalid attribute (incl. JSON `null`) | DENY | `malformed_request` | `""` |
| Cedar internal evaluation error | DENY | `evaluation_error` | evaluated version (Cedar *was* reached) |
| No policy loaded at all | DENY | `no_policy_loaded` | `""` |

Every row preserves `ExecutionID` unchanged. `PolicyVersion` is never backfilled
from "whatever is currently active" — it is exactly the version evaluated.

## How evaluation is ordered (`internal/decision/engine.go`)

1. **Request-shape validation** → malformed rejections before anything else.
2. **Identity validation** → invalid identity rejections.
3. **Unknown-tool gate** (`Known == false`) → deny, Cedar untouched.
4. **Risk-required gate** (`Known && empty Risk`) → deny.
5. **No-policy-loaded gate** → deny.
6. **Build `policy.EvalInput`** and evaluate via Cedar; any Cedar
   `HadError` → DENY regardless of Cedar's own result.

Because validation runs *before* Cedar, an adversarial input can never reach the
policy evaluator in a state that could surprise it into an allow.

## Trust provenance (read this)

`decision.Engine` performs **no signature or token verification of its own and
never will** — it trusts every field on `Request` exactly as given. Trust
provenance for v1 is *structural*: the only legitimate production caller is the
future `internal/authz` ext_authz layer, which must populate `Identity` only
from claims agentgateway already validated. Every other path (including the G1
mock) is a test/integration fixture, not an authenticated production request.

## Policy versions are content-addressed

`internal/policy.LoadFromBytes(src)` computes the policy version as the hex
SHA-256 of the source bytes (`engine.go:115`). Two identical policy texts always
produce the identical version regardless of where they were loaded from — file,
Postgres row, or an uncommitted edit. This is the provenance scheme the whole
lifecycle (below) builds on.

## Next

- [Wire contract](../reference/wire-contract.md) — the exact JSON over `POST /evaluate`.
- [Policy lifecycle](./policy-lifecycle.md) — how policies get *into* the engine.