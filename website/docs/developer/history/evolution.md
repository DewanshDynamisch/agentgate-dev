---
title: Evolution
sidebar_position: 1
description: How AgentGate got from an empty repo to a governance-driven authorization core.
---

# Evolution

The durable story is in the checkpoint folders and closure summaries
(`docs/PHASES/G{N}_WORKSTREAMS/`). This page is the narrative index — pair it
with the [commit index](./commit-index.md) for exact hashes. "POC" artifacts
from the pre-checkpoint phase are history: they are not authoritative.

## Phase 0 — bootstrap (2026-08-22)

- Repository bootstrap, AI-assisted development workflow doc, CI baseline
  (`.github/workflows/ci.yml`, validated at
  [`docs/DEVELOPMENT/CI_BASELINE.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DEVELOPMENT/CI_BASELINE.md)).
- Planning for the first enforcement slice; technology stack and project
  definition documents established.

## Phase 1 — decision core takes shape (2026-09-07 → 2026-09-11)

- Go module `github.com/Dynamisch-LLC/agentgate` scaffolded with config,
  structured logging, HTTP server (health/readiness), and graceful shutdown.
- Cedar confined to `internal/policy` — the **only** package allowed to import
  Cedar validation/evaluation
  ([`PRODUCTION-INVARIANTS.md §2`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/SECURITY/PRODUCTION-INVARIANTS.md)).
- Day-1 pinned the production invariants; Day-2 added the decision core with
  the eight reason-code contract; policy context-isolation tests landed,
  followed by G1 planning.

## G1 — Authorization Contract Freeze (2026-09-11 → 2026-09-12, **PASS/CLOSED**)

Five workstreams converged on the frozen `decision.Request`/`decision.Result`
contract and the transport JSON shape:

- **Go Backend:** the real `decision.Engine` + `internal/policy` (Cedar),
  `internal/mockauthz` test-only transport, `cmd/g1-mock-authz`.
- **Gateway/MCP:** reviewable `agentgateway` config + an independent harness
  asserting fixtures over real HTTP; the O-008 contract-mismatch finding.
- **Frontend/UI:** typed contract models, fixtures, never-throwing parsers, and
  the `OperationState` machine (structural proof that `denied` can never be
  `succeeded`).
- **QA/Security:** the out-of-process `g1blackbox` suite (builds the real mock
  binary). The independent null-as-zero-value bypass became a regression test,
  and the wire response was corrected to **always emit all five fields**.
- **DevOps:** the G1 topology + `DEVOPS_G1_ENVIRONMENT.md` (g1 compose was
  written but, in the sandbox, never run — smoke was direct-process).

Key freeze corrections from the review loop: `DisallowUnknownFields` at the
transport, the explicit-null rejection, and `always-emit` fixed the three
classes of contract drift found independently by three workstreams.

## G2 — Identity + Tool Governance (2026-09-13, **closed**)

- `internal/identity` claims mapper (fail-closed on missing/malformed/ambiguous
  claims, empty roles); `internal/toolregistry` (tool identity = backend/name,
  explicit risk, canonical SHA-256 schema fingerprint, drift detection);
  `internal/argdecl` declaration registry + `internal/contextassembly` adapter.
- **O-006 resolved** (Lead Architect approved): per-tool typed argument
  whitelist; undeclared args are structurally invisible to Cedar; explicit
  `null` rejected.
- Gateway evidence (`g1` config verified against the real binary), negative
  configs, frontend governance fixtures, and the `g2security` abuse suite.

## G3 — Policy Persistence + Governance API (2026-09-13, **closed**)

- `internal/policystore` — `Store` interface with Memory and Postgres
  implementations sharing one behavior test; embedded schema
  (partial unique index → at most one active policy per workspace).
- `internal/policymanager` — content-addressed versions (SHA-256),
  candidate→active→historical lifecycle, **atomic activation** with a
  concurrency-safe cached engine, rollback, preview, startup
  no-policy-loaded denial.
- `internal/govapi` — admin-authenticated REST API (constant-time key compare).
- Frontend governance client/state/views; `g3governance` QA suite; the `deploy/g3`
  reproducible Postgres topology + migration-verification script.

## G4 — Governance Workflow Integration (2026-09-14, **PASS/CLOSED/FROZEN**)

First time a persisted governance change **provably drives live decisions**:

```text
candidate → validate → dry-run compare → activate → observe decision change → rollback → observe restore
```

- `internal/auditevents` mutation event contract + listener interface
  (activate / rollback / create_candidate) wired into the manager
  (backward-compatible `NewWithListener`).
- `internal/governanceintegration.GovernanceDecisionService` —
  `EvaluateWithActivePolicy` + `DryRunCompare` (active-vs-candidate, zero
  mutation of the active engine).
- `POST …/policies/{version}/dryrun` endpoint; frontend dry-run models, client
  method, lifecycle-state `dryRunResult`/`rollbackStatus`, and comparison view
  renderers (63 Vitest tests passing).
- QA `g4integration` suite proving 8 DoD invariants (lifecycle propagation,
  dry-run isolation, failed-activation resilience, immediate cache coherence,
  workspace isolation, mutation-event correlation, admin-auth boundary,
  fail-closed missing policy) with zero regressions.
- `deploy/g4` integrated compose topology with secret-interpolated credentials
  and an operational guide.
- **Handoff:** G5 — Durable Audit Boundary (O-002 concrete implementation).

## G5 — Durable Audit Boundary (2026-09-14, **PASS/CLOSED/FROZEN**)

Formally approved by the Lead Architect 2026-09-14
([`CLOSURE_SUMMARY.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PHASES/G5_WORKSTREAMS/CLOSURE_SUMMARY.md)).
First checkpoint where a durable, tamper-evident boundary holds the audit
evidence **after the process exits** — and fail-closed if it cannot:

- **Durable audit boundary (`internal/audit`)** — append-only `audit_events`
  ingestion + Postgres persistence, SHA-256 row chaining (each row hashes the
  prior row → tamper-evident), independent `ChainVerifier` (recomputes the
  chain from first row, detects any gap/rewrite), pre-persistence argument
  redaction (full/hash/omit + sensitive-key overrides), fail-closed
  enforcement (audit failure ⇒ decision `DENY`).
- **O-002 resolved** (formally by the Lead Architect 2026-09-14): durable
  tamper-evident persistence is the boundary that failed-closed audit
  enforcement was waiting for.
- **QA/DevOps:** `g5audit` QA suite (9 DoD invariants incl. tamper-detection,
  chain verification, redaction, fail-closed, RBAC separation), independent
  `ChainVerifier` proving tamper-evidence out-of-process, deploy/g5 reproduceable
  Postgres topology + migration script.
- **Handoff:** G6 — Real MCP end-to-end enforcement (O-008 concrete
  implementation: wire durable audit into live gateway/MCP enforcement;
  real-binary MCP E2E gate).

## Where each gate's history lives

| Milestone | Durable record |
|---|---|
| Plan + Phase 0 | `docs/PHASES/archive/`, `docs/DEVELOPMENT/CI_BASELINE.md` |
| Decision core + contract | `docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md`, `CLOSURE_SUMMARY.md` |
| G1–G4 | each checkpoint's `G{N}_WORKSTREAMS/` folder: reference, contracts, evidence, `CLOSURE_SUMMARY.md` |
| Design rationale | `docs/PROJECT_DEFINITION.md` (decisions log), `docs/DECISIONS/OPEN_DECISIONS.md` |