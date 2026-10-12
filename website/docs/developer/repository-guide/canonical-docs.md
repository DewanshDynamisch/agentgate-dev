---
title: Canonical Repository Docs
sidebar_position: 2
description: The docs/ tree is the authoritative project context — this is its map.
---

# Canonical Repository Docs

The repository's **`docs/`** tree is the single source of truth for AgentGate
project context. This docs site synthesizes that context against the current
source, but *the `docs/` tree is authoritative and always wins* if the two
disagree. Read
[`docs/README.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/README.md)
first — it defines the five kinds of documents and is the navigation map.

```text
(client-agentgate-repo)/docs/
├── README.md                          the map — start here
├── PROJECT_DEFINITION.md              [core] the product: scope, architecture, decisions log, what's NOT built
├── TECH_STACK.md                      [core] technology choices + rationale
├── DECISIONS/OPEN_DECISIONS.md        [living] unresolved questions (O-001…O-008); resolved ones have records
├── SECURITY/PRODUCTION-INVARIANTS.md  [living, BINDING] the security contract
├── DEVELOPMENT/
│   ├── CURRENT_STATUS.md              [living] where the project is right now
│   ├── SETUP.md                       [living reference] local setup for agentgate/
│   ├── CI_BASELINE.md                 [living reference] what CI checks and why
│   └── OSS_READINESS.md               [living backlog] before public launch
├── PHASES/
│   ├── AGENTGATE_V1_10_DAY_PARALLEL_TEAM_EXECUTION_PLAN.md   [ACTIVE STRATEGY]
│   ├── AGENTGATE_V1_15_DAY_PRODUCTION_PLAN.md                 [superseded, kept]
│   ├── DAY-01-TASK-01.md / DAY-02-TASK-02.md                  [complete, kept]
│   ├── G1_WORKSTREAMS/  (contracts, closure summary, evidence — CLOSED checkpoint)
│   ├── G2_WORKSTREAMS/  (closure summary — CLOSED checkpoint)
│   ├── G3_WORKSTREAMS/  (closure summary — CLOSED checkpoint)
│   ├── G4_WORKSTREAMS/  (task tickets — IN PROGRESS checkpoint)
│   └── archive/         permanently historical plans/snapshots
└── prompts/             gitignored working material (raw prompt text)
```

## How to read it (the five kinds)

1. **Core context** (`PROJECT_DEFINITION.md`, `TECH_STACK.md`) — rare changes,
   read once, cold.
2. **Living status/decisions** (`CURRENT_STATUS.md`, `OPEN_DECISIONS.md`,
   `PRODUCTION-INVARIANTS.md`) — always current, rewritten in place; no
   append-only history.
3. **Strategy** — exactly one active plan; superseded plans carry an explicit
   banner and are never deleted.
4. **Checkpoint folders** (`G{N}_WORKSTREAMS/`) — one self-contained folder per
   checkpoint, identical shape each time: a `00_G{N}_CHECKPOINT_REFERENCE.md`,
   per-workstream detailed tickets, durable contracts/evidence, and a
   `CLOSURE_SUMMARY.md` written after merge.
5. **Closure summaries** — the durable "what shipped" record, including a
   codebase walkthrough with at least one diagram.

## The documents that govern day-to-day work

| Question | Read |
|---|---|
| "What are we building and why?" | `docs/PROJECT_DEFINITION.md` |
| "What is true right now?" | `docs/DEVELOPMENT/CURRENT_STATUS.md` |
| "What plan is in force?" | the strategy doc with no "superseded" banner (10-day parallel plan) |
| "Is there an open decision blocking me?" | `docs/DECISIONS/OPEN_DECISIONS.md` |
| "What security invariant must I satisfy?" | `docs/SECURITY/PRODUCTION-INVARIANTS.md` |
| "What's frozen in the authorization contract?" | `docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md` |
| "How do I set up and run locally?" | `docs/DEVELOPMENT/SETUP.md` |
| "What happened at checkpoint G?N?" | that checkpoint's `CLOSURE_SUMMARY.md` |

## Reading discipline

- **Never reintroduce a rejected design** ($12 decisions log in
  `PROJECT_DEFINITION.md` is binding).
- **Do not guess past an open decision** — record the conflict and continue
  unrelated work.
- **`PRODUCTION-INVARIANTS.md` is binding** — no later work may contradict it
  without an explicit, recorded exception accepted by the Lead Architect.
- Parts of the tree under `archive/`, or marked "superseded by", are history —
  read them to understand *why*, never as current instruction.