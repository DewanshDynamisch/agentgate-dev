---
title: Development Workflow
sidebar_position: 1
description: The checkpoint-based, AI-assisted development model used by this repo.
---

# Development Workflow

AgentGate is developed through **bounded tasks executed by coding agents**
(Claude Code and other agents) under the direction of a **Lead Architect**
(ChatGPT) and a human coordinator. The workflow is frozen in
[`/WORKFLOW.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/WORKFLOW.md)
at the repo root — read that file; this page is the summary.

## The loop

1. A **planning strategy** is chosen (currently the
   [10-day parallel team execution plan](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PHASES/AGENTGATE_V1_10_DAY_PARALLEL_TEAM_EXECUTION_PLAN.md)).
2. Work is split into **parallel workstreams** (Go Backend, Gateway/MCP,
   Frontend/UI, QA/Security, DevOps), each given a detailed task ticket under a
   checkpoint folder.
3. Workstreams synchronize at **integration gates** (G1, G2, G3, …) — explicit
   contract/evidence milestones, not daily handoffs.
4. At each gate, the responsible stream freezes a contract or produces
   evidence; the Lead Architect reviews; QA independently verifies.
5. When a checkpoint's branches merge, a **`CLOSURE_SUMMARY.md`** is written
   into that checkpoint's folder — the durable, human-readable record of what
   shipped, including a codebase walkthrough with diagrams.

## Workstreams (current plan)

| Workstream | Owns | Must not own |
|---|---|---|
| Go Backend | identity, tool governance, policy lifecycle, decisions, audit | MCP protocol |
| Gateway/MCP | agentgateway config, harness, real MCP path | policy/audit logic |
| Frontend/UI | operator workflow, typed contract layer | direct enforcement |
| QA/Security | independent proof of security & behavior | architecture changes |
| DevOps | reproducible build/deploy/runtime envs | security-policy weakening |

## Tasks and checkpoints

- Each checkpoint `G{N}` has `docs/PHASES/G{N}_WORKSTREAMS/` with a
  definition-of-done (`00_G{N}_CHECKPOINT_REFERENCE.md`) and per-workstream
  tickets (`01_GO_BACKEND…`, `02_GATEWAY…`, `03_FRONTEND…`, `04_QA…`, `05_DEVOPS…`).
- A task is **not complete because it compiles** — it needs implementation
  + tests + security/integration tests where relevant + documentation updates
  + status update + a review of the final diff
  ([`CLAUDE.md` → Implementation requirements](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/CLAUDE.md)).
- Coding agents must **stop at architectural boundaries**: if a task requires an
  unresolved architectural/security decision, report it rather than deciding
  silently.

## Rules that apply to every change

- **Freeze contracts before implementations.** Shared payloads are frozen at
  gates; changing them afterward requires Architect review and dependent-test
  updates.
- **Mocks are temporary parallelization tools** — never permanent substitutes
  for the real enforcement path.
- **Security boundaries cannot be bypassed for parallelism** (no gateway
  bypass, no fail-open, no silent audit loss, no raw token forwarding).
- **The Architect owns cross-stream decisions.**
- **Scope discipline:** work only on the assigned task; record (don't expand
  into) other issues you find.

## The frontend + running this repo

For practical commands, see [Local setup](../getting-started/local-setup.md)
and [Testing](./testing.md). For the state of the project, see
[Current Status](./current-status.md).