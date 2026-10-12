---
title: Fixture Policy
sidebar_position: 4
description: The canonical G1 Cedar policy fixture — roles, risk levels, and the one rule that intentionally breaks.
---

# Fixture Policy

`internal/fixturepolicy` (`agentgate/internal/fixturepolicy/fixturepolicy.go`)
is the canonical G1 **test/dev** Cedar policy set — sharing it between the
decision engine's unit tests and the mock means every consumer exercises
identical, well-understood behavior instead of two drifting copies. It is a
fixture, **not** a production policy source (production policies are versioned
store rows).

## Entity identifiers (exported constants)

| Constant | Value | Purpose |
|---|---|---|
| `RoleReader` | `reader` | may invoke any tool with `risk == read` |
| `RoleAdmin` | `admin` | may invoke `read` **or** `write` tools |
| `RolePayer` | `payer` | may invoke a `write` tool when `context has amount` and `amount <= 1000` (argument-dependent fixture) |
| `RoleBroken` | `broken` | deliberately triggers a **genuine** Cedar evaluation error (no `has` guard on `context.amount`) |
| `RiskRead` / `RiskWrite` / `RiskDestructive` | `read` / `write` / `destructive` | tool risk levels |
| `ArgAmount` | `amount` | the declared argument attribute read by the `payer`/`broken` rules |

All policy reads the fixed entities `AgentGate::Role::"…"`,
`AgentGate::Action::"InvokeTool"`, and the resource attribute `resource.risk`
(or a `resource.risk == …` compare).

## The rules, decoded

```cedar
permit(principal in AgentGate::Role::"reader", action == AgentGate::Action::"InvokeTool", resource)
  when { resource.risk == "read" };

permit(principal in AgentGate::Role::"admin",  action == AgentGate::Action::"InvokeTool", resource)
  when { resource.risk == "read" || resource.risk == "write" };

permit(principal in AgentGate::Role::"payer",  action == AgentGate::Action::"InvokeTool", resource)
  when { resource.risk == "write" && context has amount && context.amount <= 1000 };

forbid(principal, action == AgentGate::Action::"InvokeTool", resource)
  when { resource.risk == "destructive" };

permit(principal in AgentGate::Role::"broken", action == AgentGate::Action::"InvokeTool", resource)
  when { context.amount > 10 };
```

| Role × risk | Outcome |
|---|---|
| `reader` × `read` | ALLOW (`policy_allow`) |
| `reader` × `write` / `destructive` | DENY (`no_matching_policy`) |
| `admin` × `read` / `write` | ALLOW |
| `admin` × `destructive` | DENY (`policy_deny` — the **explicit** forbidden fixture, distinct from Cedar's structural default-deny) |
| `payer` × `write`, `amount` present & `<= 1000` | ALLOW (argument-dependent) |
| `payer` × `write`, `amount` absent | DENY (`no_matching_policy`) |
| any × `destructive` | DENY (`policy_deny`) |
| `broken`, no `amount` attribute | DENY (`evaluation_error`) — a real Cedar error, not a simulated one |
| any unlisted role | DENY (`no_matching_policy`) |

## Why the `broken` rule exists

The `broken` rule references `context.amount` **without** a `has` guard, so a
request from the `broken` role that omits `amount` produces a genuine Cedar
evaluation error — exercising the `evaluation_error` → DENY path against a real
engine failure, which is exactly the fail-closed behavior
[PRODUCTION-INVARIANTS §2](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/SECURITY/PRODUCTION-INVARIANTS.md)
requires and CI/tests pin.

## How the fixture is used

- `internal/decision.Engine` unit tests — behavior against the canonical rules.
- `cmd/g1-mock-authz` / `internal/mockauthz` — the mock wraps an engine loaded
  from `fixturepolicy.CedarSource`, so G1 blackbox and the gateway harness
  exercise the same rule set over real HTTP.
- `gateway/fixtures/` wire bodies reference these role/tool identifiers.

To write fixture requests: `roles` are `Role*`, `classification.risk` is a
`Risk*`, and `arguments.amount` (`int`) is what the payer rule checks.