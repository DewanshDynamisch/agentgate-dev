---
title: Open Decisions
sidebar_position: 6
description: The unresolved architectural questions (O-001…O-008) that may block or constrain implementation.
---

# Open Decisions

Source of truth:
[`docs/DECISIONS/OPEN_DECISIONS.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DECISIONS/OPEN_DECISIONS.md).

> **An open decision is not an invitation for coding agents to guess.** If
> assigned work runs into one, stop the decision-dependent part and record the
> conflict, or pick the explicitly "allowed development approach" — never both.

## Open

| ID | Priority | Question | Current action while open |
|---|---|---|---|
| **O-001** | Critical | Downstream identity/credential propagation — the mechanism for obtaining a downstream credential (required caller/on-behalf-of identity, audience, lifetime, permissions) instead of forwarding the inbound bearer. | Build interfaces/test doubles around the credential boundary; no unsafe token passthrough anywhere. |
| **O-003** | High | agentgateway conformance/security boundary — the exact behaviors relied upon must be verified, not assumed from feature availability. | Build targeted integration tests during the first E2E slice. |
| **O-004** | High | Supported MCP revision(s) — plan targets `2026-07-28`; older-client compatibility unresolved. | Confirm the compatibility boundary before finalizing client/backend integration. |
| **O-005** | High | Tool identity + schema fingerprint — precise canonicalization/fingerprinting rule; a fingerprint change must cause governance review and deny-by-default until classified. | G2 built `toolregistry.FingerprintSchema`/`CheckDrift` (the deterministic rule exists); the *governance-review workflow* consumption remains open. |
| **O-007** | Medium/High | AgentGate execution identity — must not inherit protocol-session assumptions if the MCP revision is stateless; AgentGate should own a correlation id for audit and request grouping. | Pending the request-context model. `execution_id` already flows through the decision contract. |
| **O-008** | High | ext_authz → `decision.Request` transport mapping — agentgateway's HTTP/gRPC `ext_authz` can't construct the bespoke JSON body; the real `internal/authz` translation mechanism is undesigned. | **Gateway/MCP must NOT build an adapter/shim to bridge this.** Keep the direct-HTTP mock/harness pattern until `internal/authz` exists. See [Gateway integration](./../architecture/gateway-integration.md). |

## Resolved

| ID | Priority was | Resolution |
|---|---|---|
| **O-002** (2026-09-09) | Critical | Audit durability invariant: an ALLOW must not return unless a durable audit outcome is guaranteed; audit-durability failure fails closed. Binding in `PRODUCTION-INVARIANTS.md §5`/§12.2. Mechanism (durable buffer) is implementation, not an open question. |
| **O-006** (2026-09-13) | High | Per-tool typed argument-declaration registry (`internal/argdecl`) — explicit whitelist, only declared args reach Cedar, `null` rejected, missing required fails closed. See [Argument authorization](./../architecture/argument-authorization.md). |

## Decision protocol (how these close)

1. State the question. 2. Identify security/product/implementation impact.
3. Verify external facts. 4. Choose a solution. 5. Record the rationale.
6. Update affected architecture/plan documents. 7. Only then remove from this file.

**Open decisions must not be silently resolved in code** — resolving one is an
architectural act owned by the Lead Architect, not a side effect of
implementation.