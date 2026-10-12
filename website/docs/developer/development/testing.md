---
title: Testing
sidebar_position: 3
description: The layered test strategy — unit tests, independent QA suites, and how Postgres-gated tests run.
---

# Testing

AgentGate deliberately layers its verification: every behavior is pinned by
*unit tests*, then re-proven out-of-process by *independent QA suites* that
cannot lean on package internals, then again inside the *deploy environments*.

## Layer 1 — Go unit tests (`agentgate/internal/**/*_test.go`)

One test file per package alongside the code, including
fail-closed/negative-path coverage everywhere (config, decision, policy,
httpserver, identity, toolregistry, argdecl, contextassembly, policystore,
policymanager, govapi, governanceintegration, auditevents, mockauthz, logging).

Repository-verified negative cases include: no-policy-loaded denies,
unknown-tool denies, ambiguous/absent identity, missing required arguments,
explicit `null` rejection, activation-failure leaves last-good active, failed
rollback leaves active unchanged, admin auth rejects missing/wrong keys, and
Cedar evaluation errors → DENY.

```bash
cd agentgate
go test ./...            # all unit packages
go test ./... -race      # race detector on the concurrency-heavy packages
```

**Postgres-gated store tests** (`internal/policystore`): the *same* behavior
suite runs against `MemoryStore` and `PostgresStore`
(`TestPostgresStore`), so the SQL backend inherits exactly the in-memory
invariants. They skip unless configured:

```bash
$env:AGENTGATE_TEST_POSTGRES_URL="postgres://agentgate:postgres@localhost:5432/agentgate_gorm?sslmode=disable"
go test ./internal/policystore/ -run Postgres -v
```

Set this up with the g3 topology — [see Deployment environments](./deployment-environments.md).

## Layer 2 — independent QA suites (`agentgate/qa/`)

Deliberately **outside** `internal/`; they exercise the shipped surfaces, not
package helpers. Two styles:

| Suite | Style | What it proves |
|---|---|---|
| `g1blackbox` | out-of-process: `TestMain` **builds the real `g1-mock-authz` binary**, runs it as a subprocess, and talks plain HTTP over `POST /evaluate` | the frozen wire contract end-to-end: all 8 deny reasons, allow, transport errors, policy-version carry-through |
| `g2security` | in-process against `identity`/`toolregistry`/`argdecl`/`contextassembly` | the authoritative abuse matrix: every failure class, unknown/`duplicate`-risked tools, nulls, type mismatches — nothing slips to DECIDE |
| `g3governance` | in-process `httptest` wrapping the real `govapi` handler + `policymanager` | lifecycle invariants over HTTP: candidates→activate atomicity, rollback, admin auth (missing/wrong key → 401), content-hash idempotency, no-mutation validation |
| `g4integration` | in-process `httptest` wiring **governance → decision** through the real service | the G4 proof: activating a policy changes decisions; dry-run compare reports active-vs-candidate with `changed` for both directions; atomic swap / cache coherence |

```bash
cd agentgate
go test ./qa/...          # all four suites
```

## Layer 3 — gateway harness (`gateway/harness/`, independent module)

The interoperability proof: an independent Go module (own `go.mod`, zero
imports of agentgate code) that re-defines the wire shape for itself and asserts
the 8 wire fixtures + G2 fixtures over real HTTP against a **running mock**.
When the mock is down, `TestG1Scenarios` skips with explicit instructions
(`G1_MOCK_AUTHZ_HARNESS_ADDR` env or the default addr):

```bash
cd agentgate && go run ./cmd/g1-mock-authz     # terminal 1, :8091
cd gateway/harness && go test ./... -v          # terminal 2
go run ./cmd/g1report                           # human table over the same fixtures
```

## Layer 4 — frontend (`frontend/`, Vitest)

TypeScript contract layer validation: never-throwing parsers, the 8-reason wire
fixtures (incl. deliberately invalid bodies), discriminated error model, state
machines that prove `succeeded` cannot be produced by failure paths, and the
governance store (activation/rollback/dry-run status flow).

```bash
cd frontend
npm install
npm test              # vitest run
npm run typecheck     # tsc --noEmit (strict)
```

## Layer 5 — deploy environments

The end-to-end proof in realistic topologies lives in `deploy/` (g3 Postgres +
migration verification, g4 full governance-to-decision E2E). See
[Deployment environments](./deployment-environments.md).

## CI

GitHub Actions runs the Go baseline (fmt, vet, race tests, lint, vulncheck)
without the Postgres-gated or external-service suites — those need live
resources and are run manually in deploy topologies. See [CI](./ci.md) for the exact matrix.