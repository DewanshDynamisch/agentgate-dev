---
title: Components
sidebar_position: 2
description: Package-by-package map of the AgentGate Go module and the other working areas.
---

# Components

This page is a location map for the codebase: what each package owns, where it
lives, and its key entry point. Prefer it to guessing from file names. All
paths are relative to the repo root unless stated otherwise.

## AgentGate Go module (`agentgate/`)

| Package | Responsibility | Key entry points |
|---|---|---|
| `cmd/agentgate` | Production entry point: wires config → logging → HTTP server → (memory or Postgres) store → policy manager → governance integration → governance API, then serves with graceful shutdown. | `run()` at `agentgate/cmd/agentgate/main.go:33`; store selection at `main.go:47-65`. |
| `cmd/g1-mock-authz` | **Non-production** JSON/HTTP wrapper around the real decision core, used for cross-workstream integration and QA. Never started from `cmd/agentgate`. | `run()` at `agentgate/cmd/g1-mock-authz/main.go:40`. |
| `internal/config` | Typed, validated `AGENTGATE_*` env configuration; fail-fast on invalid values. | `Config` at `internal/config/config.go:31`; `Load()` at `config.go:62`. |
| `internal/logging` | Thin stdlib `log/slog` JSON logger. | `New(level, w)` at `internal/logging/logging.go:18`. |
| `internal/httpserver` | The `:8090` HTTP listener: health/readiness + the route-registration seam; graceful shutdown. | `Server.New()` at `internal/httpserver/server.go:32`; `/healthz` & `/readyz` at `server.go:41-42`. |
| `internal/decision` | The frozen authorization decision core: `Request`/`Result` types and the fail-closed `Evaluate` flow. |
| `internal/policy` | The **only** package importing `cedar-go`. Loads Cedar policies, content-hash versions them, evaluates a narrow fixed entity model. | `LoadFromBytes()` at `internal/policy/engine.go:115`; `Evaluate()` at `engine.go:139`. |
| `internal/identity` | Claims → typed identity mapping (agent + on-behalf-of + roles) with four fail-closed failure classes. | `NewMapper()` at `internal/identity/mapper.go:144`; `Map()` at `mapper.go:176`. |
| `internal/toolregistry` | Tool identity, risk classification, deterministic schema fingerprinting, drift detection; authoritative tool-governance boundary. | `Registry`/`NewRegistry` at `internal/toolregistry/toolregistry.go:263/270`; `CheckDrift` at `:182`. |
| `internal/argdecl` | Per-tool typed argument declarations (whitelist); undeclared args are structurally invisible to policy (O-006). | `NewDeclarationSet` at `internal/argdecl/argdecl.go:99`; `Resolve` at `:173`. |
| `internal/contextassembly` | Assembles validated identity + governance record + resolved args into a `decision.Request`; fail-closed on every invalid input. | `Assemble()` at `internal/contextassembly/assembler.go:84`. |
| `internal/policystore` | Versioned, workspace-isolated policy persistence (`candidate`/`active`/`historical`), atomic activation; Postgres + in-memory impls sharing one behavior test. | `Store` at `internal/policystore/store.go:10`; migration `internal/policystore/migrations/001_create_policies.sql`. |
| `internal/policymanager` | Lifecycle orchestration: validate, create candidate, activate/rollback with atomic in-memory engine swap, preview; emits mutation audit events. | `New()` at `internal/policymanager/manager.go:34`; `Activate()` at `:98`; `Rollback()` at `:132`. |
| `internal/govapi` | Admin-authenticated governance REST API (validate / create / list / get / activate / rollback / preview / dryrun). | `RegisterRoutes` at `internal/govapi/handler.go:34`; `AdminAuthMiddleware` at `middleware.go:11`. |
| `internal/governanceintegration` | Governance-to-decision bridge: evaluate against the active policy, and dry-run compare active-vs-candidate without mutation. | `EvaluateWithActivePolicy` at `internal/governanceintegration/integration.go:34`; `DryRunCompare` at `:48`. |
| `internal/auditevents` | Policy-mutation audit event contract + in-memory listeners (durable audit is later). | `MutationEvent`/`MutationListener` at `internal/auditevents/events.go:25/37`. |
| `internal/mockauthz` | JSON wire layer for the G1 mock (strict unknown-field rejection, all-fields-always-present results). | `ServeHTTP` at `internal/mockauthz/handler.go:52`; wire types in `wire.go`. |
| `internal/fixturepolicy` | The canonical G1 Cedar fixture policy with exported role/risk/arg constants. | `CedarSource` at `internal/fixturepolicy/fixturepolicy.go:47`. |
| `internal/audit` | Append-only Postgres audit persistence, tamper-evident SHA-256 hash chaining, pre-persistence argument redaction, independent `ChainVerifier`, and fail-closed decision enforcement (O-002). | `Service` at `internal/audit/service.go:28`; `PostgresStore` at `postgres.go:42`; `ChainVerifier` at `verifier.go:30`. |
| `qa/g1blackbox`, `qa/g2security`, `qa/g3governance`, `qa/g4integration`, `qa/g5audit` | Independent QA suites: run the built binary/sub-system over real HTTP/DB and assert behavior — they import **no** `internal/*` package. |

## Gateway/MCP (`gateway/`)

| Path | What it is |
|---|---|
| `gateway/config/g1-agentgateway.yaml` | Reviewable `agentgateway` config for the single governed route that calls AgentGate via `policies.extAuthz`. |
| `gateway/harness/` | An **independent Go module** (own `go.mod`) that POSTs wire fixtures to the running mock and asserts the enforcement gate (`WouldForwardToBackend` is true only on http-200 + `ALLOW`). |
| `gateway/fixtures/` | 8 root wire fixtures (allow/deny/error/transport) + G2 fixtures (identity claims, tool metadata). |
| `gateway/docs/` | G2 evidence docs (inspection, ext_authz extraction evidence, negative enforcement matrix, G6 handoff). |

## Frontend contract layer (`frontend/`)

The production admin console is the React/Vite app in `frontend/app/` (decision
O-009). The root `frontend/` package is a TypeScript **library**: typed governance API models and
clients (`src/api/governanceClient.ts`, `src/models/governance.ts`), frozen
decision wire models + parsers (`src/models/decision.ts`, `src/parsing/`),
lifecycle state stores (`src/state/`), framework-free view renderers
(`src/view/`), and committed wire/UI fixtures. Tests via Vitest.

## Deploy environments (`deploy/`)

| Path | What it is |
|---|---|
| `deploy/g1/` | G1 contract-test topology targeting `cmd/g1-mock-authz` (files state "written but never run"; the verified path was direct-process — see [Deployment environments](../development/deployment-environments.md)). |
| `deploy/g2/` | G2 "reviewable target topology": `agentgate` image + config fixtures + negative configs. |
| `deploy/g3/` | Reproducible Postgres topology: `postgres:16-alpine` + `agentgate` + migration-verification script. |
| `deploy/g4/` | Integrated governance-to-decision E2E topology (reuses the g3 Dockerfile) with a full curl lifecycle runbook. |
| `deploy/g5/` | Reproducible Postgres topology with database privilege separation (`agentgate_app` vs `agentgate_migrator`) and audit immutability triggers. |

## Canonical documentation (`docs/`)

The durable project context drives everything — see
[Canonical repository docs](../repository-guide/canonical-docs.md).