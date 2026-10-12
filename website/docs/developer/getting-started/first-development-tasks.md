---
title: First Development Tasks
sidebar_position: 4
description: A recommended order for making your first changes to the AgentGate repo.
---

# First Development Tasks

This page suggests a safe, ordered path into the codebase. The project is
developed through **bounded tasks assigned per checkpoint workstream** — see
[Development workflow](../development/workflow.md) — so "first tasks" here are
about getting oriented and landing something small, not about finding a hobby
project to drive the roadmap.

## 0. Read before you touch anything (non-negotiable)

The repository has explicit operating rules that coding agents and humans both
follow. Skipping these is how subtle architecture drift starts:

1. [`CLAUDE.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/CLAUDE.md)
   — coding-agent operating rules (read before making changes).
2. [`WORKFLOW.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/WORKFLOW.md)
   — roles, the checkpoint loop, per-checkpoint documentation requirement.
3. [`docs/README.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/README.md)
   — the documentation map; it points to everything else and says which plan is
   currently active.
4. [`docs/PROJECT_DEFINITION.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PROJECT_DEFINITION.md)
   — what we are building and, equally important, what we are *not*.
5. [`docs/SECURITY/PRODUCTION-INVARIANTS.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/SECURITY/PRODUCTION-INVARIANTS.md)
   — binding; no later work may contradict it without a recorded exception.
6. [`docs/DECISIONS/OPEN_DECISIONS.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DECISIONS/OPEN_DECISIONS.md)
   — open questions are **not** invitations to guess.

## 1. Get oriented (half a day)

- Work through [Local setup](./local-setup.md) and complete the smoke-test
  checklist.
- Run the independent QA suites to see how verification is done
  ([Testing](../development/testing.md)):
  ```bash
  cd agentgate
  go test ./qa/g1blackbox/... -v     # out-of-process black-box suite
  go test ./internal/decision/... -v
  ```
- Read [System architecture](../architecture/system-overview.md) and the
  [Decision core](../architecture/decision-core.md) page — they map the code
  layout to the concepts.

## 2. Land a small, verifiable change

Good first changes (single package, existing test patterns, no contract change):

- **Bug/regression test first.** Pick a behavior in `internal/decision` or
  `internal/mockauthz` and write the test that pins it before touching any
  implementation. Follow the project's test-driven convention.
- **Documentation.** If a `.md` under `docs/` is stale or a code comment drifts
  from behavior, fix it — documentation accuracy is treated as a deliverable,
  not a nicety.
- **Fixture/edge case.** Add a wire fixture under `gateway/fixtures/` or a case
  to an existing table-driven test, extending coverage without changing
  semantics.
- **Frontend contract types.** Add/verify model types under `frontend/src/models/`
  with their vitest assertions — no runtime dependency, purely typed.

## 3. The golden rules you must not break

- **Do not modify the frozen contract** (`internal/decision.Request`/
  `internal/decision.Result` or the mockauthz wire shape) without Architect
  review. See the
  [G1 contract freeze](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md).
- **Do not silently resolve open decisions** (O-* in
  `docs/DECISIONS/OPEN_DECISIONS.md`). If work runs into one, stop that
  decision-dependent part, record the conflict, and continue unrelated work.
- **Never weaken fail-closed behavior** to make a test pass. If a test seems to
  require weakening security, the test is wrong.
- **Do not add dependencies** without justification in scope.
- **Scope discipline.** Work only on the assigned task. If you find another
  issue: if it blocks the task, report it; otherwise record it for later.
- **Verification is part of the task.** Run the tests required by the task; a
  task is not done merely because it compiles.

## 4. Before you push

- Read [Testing](../development/testing.md) and [CI](../development/ci.md).
- Run: `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./...` in
  `agentgate/` (CI runs `-race` on Linux plus lint and vulncheck).
- Keep the diff scoped to the task. The repo reviews diffs, not intentions —
  see the [final-diff review requirement](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/CLAUDE.md#implementation-requirements).