---
title: Frontend Contract Layer
sidebar_position: 10
description: The TypeScript contract layer that will back the operator governance UI.
---

# Frontend Contract Layer

:::note Production UI
The production admin console is the React/Vite app in `frontend/app/` — the UI
framework question was resolved by decision **O-009** (2026-09-18) in
`docs/DECISIONS/OPEN_DECISIONS.md`. This page documents the contract layer in
`frontend/src/`, which the console imports (Vite alias `@contract`).
:::

The `frontend/` directory's root package is the **typed client contract layer**
for the policy-governance UI. It was written framework-agnostic, before the UI
framework was chosen.

It is a zero-runtime-dependency TypeScript library (strict mode, ES2022,
NodeNext modules). Its tests run under Vitest.

## What it provides

**Frozen decision contract (`src/models/decision.ts`)** — the G1 wire and domain
types: `Decision` (`ALLOW`|`DENY`), the 8 deterministic `ReasonCode`s, the wire
request/result shapes, and helpers like `policyWasReached()`. These mirror
`internal/decision` exactly (fixtures are kept in sync with the Go side).

**Never-throwing parsers (`src/parsing/`)** — `parseAuthorizationResult()` and
friends enforce the contract strictly (including cross-field
decision/reason consistency) and return `ParseResult<T>`; a malformed backend
response can never crash a consumer.

**Error model (`src/models/api-error.ts`)** — a discriminated union
(`network | unexpected_status | transport_rejected | invalid_response`) so UI
consumers can reason about failure modes without guessing.

**Governance client (`src/api/governanceClient.ts`)** — the
`GovernanceClient` interface with 7 methods covering the whole lifecycle, plus
two implementations:

- `HttpGovernanceClient(baseUrl, adminToken, fetchFn)` — real HTTP; sends
  `Authorization: Bearer <adminToken>` on every request; maps `{error:{code,message}}`
  responses into typed `GovernanceApiError`s. `fetchFn` is injectable for tests.
- `MockGovernanceClient` — in-memory backend for contract-driven development,
  with the same semantics (content-hash versions, 404 on unknown versions).

| Client method | Backend endpoint |
|---|---|
| `listPolicies(ws)` | `GET /api/v1/workspaces/{ws}/policies` |
| `createCandidate(ws, content, desc)` | `POST …/policies` |
| `validatePolicy(ws, content)` | `POST …/policies/validate` |
| `activatePolicy(ws, version)` | `POST …/policies/{version}/activate` |
| `rollbackPolicy(ws, targetVersion)` | `POST …/policies/rollback` |
| `previewPolicy(ws, version, samples)` | `POST …/policies/{version}/preview` |
| `dryRunCompare(ws, version, samples)` | `POST …/policies/{version}/dryrun` (G4) |

**Lifecycle state (`src/state/`)** — pure reducers so backend state can never
be misread:

- `OperationState<TSuccess>` (G1) — `idle|loading|succeeded|denied|apiError|stale`;
  structurally proven: `fail`/`deny`/`markStale`/`start` can never yield
  `succeeded`, and a DENY decision is never confused with a denied mutation.
- `PolicyLifecycleStore` (G3/G4) — `loadPolicies`, `validateDraft`,
  `submitCandidate`, `activate`, `rollback`, `dryRunCompare` with explicit
  `activationStatus`/`rollbackStatus`/`dryRunResult`. It never implies a
  candidate is active before the backend confirms activation.

**View renderers (`src/view/`)** — pure functions producing display-safe view
objects, no DOM: decision views with `allow|deny|error|stale|pending` tones,
policy badges/summaries, and the G4 `toDryRunComparisonView` (the
"Outcome Changed: ALLOW → DENY" headline) and `toOperationStatusView`.

**Fixtures (`src/fixtures/`)** — committed raw-JSON wire bodies (all 8 deny
reasons + allow + transport + deliberately-invalid bodies), stale-state UI
fixtures, G2 governance fixtures, and governance fixtures. Deliberately typed
`unknown` so tests must push them through the parsers.

## State-of-state guarantees (why this layer exists)

The DoD for the frontend stream is about *truthfulness*: every displayed policy
state comes from the backend; candidate/active/rollback cannot be confused;
activation/rollback only appear successful after server confirmation; failed
API calls cannot produce false success. The state stores and view renderers are
the structural proof of those rules, independent of any future UI framework.

## Next

- [API reference](../reference/api-reference.md) — the endpoints the client calls.