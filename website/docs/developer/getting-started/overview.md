---
title: Overview
sidebar_position: 1
description: What AgentGate is, why it exists, and what the current codebase does.
---

# AgentGate — Overview

:::info Contributor guide
This section is for contributors to the AgentGate codebase. It was last
synchronized at checkpoint **G5** (commit `25b5ea4`); see
[Current Status](../development/current-status.md) for what that means. For
running and integrating AgentGate, use the main [Docs](/docs/intro).
:::

> **One-line description.** AgentGate is an authorization and governance layer
> for AI agent tool calls — a checkpoint in the only path between an agent and
> its tools. It decides, per tool call, whether the call is permitted, and
> records exactly which policy version made the decision.

## The problem

AI agents can now call real tools — delete records, send emails, move money, run
code. Nothing in most stacks checks, per individual tool call, whether that
specific action should be allowed, and nothing writes down why it was. Because
an agent's tool choices are driven by text it ingested (which may be
attacker-controlled via prompt injection), the blast radius of a manipulated
agent is whatever its credentials permit — usually everything.

## The mechanism

```text
AI agent  --wants to call a tool-->  AgentGate  --if allowed-->  real tool
```

The agent cannot bypass it — it is not a monitor on the side, it is a gate in
the path. At that checkpoint AgentGate does four things:

1. **Resolve identity** — which agent, acting on behalf of which human, with what roles.
2. **Decide** — allow or deny, per call, from versioned [Cedar](https://www.cedarpolicy.com/) policy.
3. **Log** — every call, allowed or denied, with full context and the exact policy version that decided it.
4. **Evolve** — an operator reviews the log and adjusts policy; the next call obeys the new rule.

For the full product rationale, non-goals, rejected options, and the
architecture decisions behind it, read the repository's canonical
**[`docs/PROJECT_DEFINITION.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PROJECT_DEFINITION.md)**
— start there before anything else.

## Key architectural commitments

- **agentgateway owns the MCP data plane.** The project deliberately does not
  build an MCP proxy. [agentgateway](https://agentgateway.dev) (a Linux
  Foundation project) handles MCP transport, routing, tool discovery, and JWT
  validation. AgentGate sits behind it as the authorization decision point and
  never speaks MCP itself.
- **Fail-closed by construction.** Missing/invalid identity denies, unknown or
  unclassified tools deny, missing policy denies, Cedar evaluation errors deny,
  malformed input can never produce an allow. These are frozen security
  invariants — see [Security model](../architecture/security-model.md).
- **Detective governance, not real-time approval.** The v1 design is
  policy-decides-instantly, everything-is-logged, humans-review-the-log-and-evolve-policy.
  Real-time human-in-the-loop was explicitly reversed out of v1.
- **AgentGate governs tool calls and callers, not resources.** Whether a caller
  may act on a specific record is the tool's own responsibility.

## Repository shape

The `agentgate-dev` monorepo has five working areas plus the canonical docs:

| Path | What it is |
|---|---|
| `agentgate/` | The Go module — the product code (decision core, identity/tool governance, policy lifecycle, governance API). |
| `gateway/` | `agentgateway` configuration + an independent verification harness for the frozen authorization contract. |
| `frontend/` | Framework-agnostic TypeScript contract layer (typed models, API client, lifecycle state, view renderers) for the policy-governance UI. |
| `deploy/` | Per-checkpoint reproducible environments (g1–g4 docker-compose topologies). |
| `docs/` | **Canonical project documentation** — the authoritative context every task is bound by. |
| `WORKFLOW.md`, `CLAUDE.md` | The development workflow and coding-agent operating rules. |

## Current status (what actually works today)

> **Living status:** the repo's own
> [`docs/DEVELOPMENT/CURRENT_STATUS.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DEVELOPMENT/CURRENT_STATUS.md)
> is the canonical answer. Read it before assuming anything. (At the time this
> page was written that file lagged the git history — the checkpoint closures
> and the [Current Status](../development/current-status.md) page here are the
> up-to-date snapshot.)

Summary of what exists and runs in the Go module:

- ✅ Go service (`cmd/agentgate`) with typed env config, structured JSON
  logging, health/readiness, graceful shutdown.
- ✅ A frozen authorization decision core (`internal/decision` + `internal/policy`
  + Cedar) with a deterministic fail-closed matrix and content-hashed policy
  versions.
- ✅ A non-production JSON/HTTP mock of the decision core
  (`cmd/g1-mock-authz`, `internal/mockauthz`) used for cross-workstream
  integration and black-box QA.
- ✅ Identity claims mapping (`internal/identity`), tool governance with schema
  fingerprinting and drift detection (`internal/toolregistry`), and a typed
  per-tool argument-declaration registry (`internal/argdecl` + `internal/contextassembly`).
- ✅ Versioned policy persistence (Postgres + in-memory), an atomic lifecycle
  manager, and an admin-authenticated governance REST API (`internal/policystore`,
  `internal/policymanager`, `internal/govapi`).
- ✅ Governance-to-decision integration with dry-run comparison
  (`internal/governanceintegration`) and mutation-audit event plumbing
  (`internal/auditevents`).

**Not yet built** (the boundary where the next work is): the production
`ext_authz` gRPC service (`internal/authz` is a placeholder), durable decision
audit (`internal/audit` is a placeholder), real MCP end-to-end enforcement, and
a shipped UI application (frontend is the typed contract layer only).

## Where to go next

- [Prerequisites](./prerequisites.md) and [Local setup](./local-setup.md) to get the codebase running.
- [System architecture](../architecture/system-overview.md) to understand how the pieces fit together.
- [Development workflow](../development/workflow.md) to learn how work is scoped and reviewed.