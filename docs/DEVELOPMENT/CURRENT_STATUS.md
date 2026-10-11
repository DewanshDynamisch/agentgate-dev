# AgentGate — Current Development Status

**Last updated:** 2026-10-09
**Release State:** v1.0.0-rc1 (Production Readiness Complete)

This file states only what is true *right now*. It is rewritten in place, not appended to — for history, see each checkpoint's own `CLOSURE_SUMMARY.md` in `docs/PHASES/G{N}_WORKSTREAMS/` or `git log`. Full navigation: `docs/README.md`.

---

## Where we are

AgentGate v1 Production Readiness (all 25 planned implementation and verification tasks) is **COMPLETE**. The codebase is frozen at **v1.0.0-rc1** on branch `feature/v1-production-readiness`.

Every subsystem, security boundary, and operational requirement defined in `docs/superpowers/plans/2026-09-29-agentgate-v1-production.md` has been implemented, tested, and verified against running container topologies:

- **CI pipeline configured:** 7 automated jobs defined in `.github/workflows/ci.yml` (`go`, `demo-module-tests`, `frontend`, `images`, `g6-live-e2e`, `demo-live-e2e`, `v1-security-gate`). GitHub CI not yet run.
- **Definitive operator runbook:** 12 operational runbook procedures documented in `docs/OPERATIONS/RUNBOOK.md`.
- **no known critical or high defects found by the 17-scenario live gate, which passed 3 consecutive from-scratch runs in the Docker sandbox on 2026-10-09; known limitations are listed in the runbook; GitHub CI has not run.**

### Checkpoint Milestones

- **G1 — Authorization Contract Freeze: PASS / CLOSED / FROZEN** (2026-09-12). Frozen `internal/decision.Request`/`Result` contract. 37-test independent black-box suite.
- **G2 — Identity + Tool Governance boundary: PASS / CLOSED** (2026-09-13). Identity claims mapper (`internal/identity`), tool registry with canonical SHA-256 schema fingerprint & drift detection (`internal/toolregistry`), typed argument whitelist (`internal/argdecl`, resolving O-006), and context assembly (`internal/contextassembly`).
- **G3 — Policy Persistence + Governance API: PASS / CLOSED** (2026-09-13). Policy store (`internal/policystore`, PostgreSQL & Memory), lifecycle manager (`internal/policymanager`, content-addressed versions, atomic activation, cached engine, rollback), and admin governance REST API (`internal/govapi`).
- **G4 — Governance Workflow Integration: PASS / CLOSED / FROZEN** (2026-09-14). Mutation audit events (`internal/auditevents`), dry-run candidate-vs-active evaluation (`internal/governanceintegration`), and `POST .../dryrun` endpoint.
- **G5 — Durable Audit Boundary: PASS / CLOSED / FROZEN** (2026-09-14). Append-only `audit_events` in PostgreSQL (`internal/audit`), tamper-evident SHA-256 row chaining (`prev_hash` + `row_hash`), independent `ChainVerifier`, pre-persistence argument redaction (`redact.go`), fail-closed audit enforcement (resolving O-002), and DB immutability triggers.
- **G6 — Real MCP End-to-End Enforcement: PASS / CLOSED / FROZEN** (2026-09-16, evidence closed 2026-09-21). Envoy v3 `ext_authz` gRPC service (`internal/authz`), JSON-RPC 2.0 tool call adapter, trusted gateway identity metadata extraction, and live integration with pinned `agentgateway:v1.4.0` (12/12 DoD scenarios passing).
- **G7 — Evidence, Downstream Identity & Read Surface: PASS / CLOSED** (2026-09-29 → 2026-10-08). Downstream scoped credential minting (`internal/credential`, resolving O-001) with ephemeral RS256 JWTs, 60s lifetime, `jti` replay protection, audited reference hashes (`downstream_credential_ref`), governance read APIs (`GET .../audit-events`, `GET .../tools`), and realistic `support-desk-mcp` demonstration backend (`deploy/demo/`).
- **G8 — Operator Visibility Complete: PASS / CLOSED** (2026-10-08). Admin UI (`frontend/app/`) fully wired to live backend APIs for Audit Logs, Tools Catalog, and Policy Lifecycle. All panels backed by real data; Vitest test suite passing (87/87 tests).
- **G9 — Integration Breadth: PASS / CLOSED** (2026-10-09). Persistent PostgreSQL tool catalog store (`internal/toolcatalog`), runtime tool registration and risk reclassification via Admin API, and pinned MCP revision `2026-07-28` (O-004).
- **G10 — Production Environment Readiness: PASS / CLOSED** (2026-10-09). Production Compose topology (`deploy/prod/docker-compose.yml`) with Keycloak 26 OIDC federation, mutual TLS (mTLS) upstream between agentgateway and agentgate, database privilege separation (`agentgate_app` vs `agentgate_migrator`), zero dev defaults in production mode, distroless container images, subcommands (`serve`, `migrate`, `verify-audit`, `healthcheck`), and Prometheus metrics on `:9090/metrics`.
- **G11 — Release Candidate Security Gate: PASS / CLOSED** (2026-10-09). Automated clean-room verification (`clean-room.ps1`, 13/13 checks) and live 17-scenario security gate suite (`run-security-gate.ps1`, `qa/v1security`) passed in the Docker sandbox on 2026-10-09, run 3 times; GitHub CI not yet run. Full end-to-end audit and release candidate commit created (`chore(release): v1.0.0-rc1 status`).

---

## Completed Production Architecture

### 1. Gateway & Envoy v3 `ext_authz` Integration
- Pinned `agentgateway:v1.4.0` (`sha256:771afaf093065477fa296eb90dcb618a0300165f12a32f80bbdd1427fab900ec`).
- Communicates with AgentGate via Envoy v3 gRPC `envoy.service.auth.v3.Authorization/Check` on port `:9001` over mutual TLS (`internal/tlsconfig` with client certificate verification).
- Inbound JSON-RPC 2.0 `tools/call` bodies are extracted from `AttributeContext_HttpRequest` up to 1 MiB (`maxRequestBytes: 1048576`).
- Fail-closed behavior implemented: any failure (outage, policy deny, malformed payload, schema drift, unauthenticated token) results in `DENY` with 0 calls forwarded to the downstream backend.

### 2. Cedar Authorization Core
- Pinned `cedar-go v1.8.0` encapsulated entirely within `internal/policy` (zero Cedar imports elsewhere).
- Policy evaluation takes frozen `decision.Request` attributes (principal identity, roles, workspace, tool name, tool risk, and whitelisted typed arguments).
- Thread-safe atomic swaps of active policy engine (`internal/policymanager`) with content-addressed SHA-256 versioning and rollback capabilities.

### 3. Identity & OIDC Admin Authentication
- Production token verification via OIDC JWKS (`internal/oidcverify`) federated with Keycloak 26.
- Strict audience enforcement (`aud: agentgate-gateway` for agents, `aud: agentgate-ops-cli` for admin CLI/UI).
- Token type validation (rejects refresh/ID token replay as access tokens).
- Fail-closed JWKS cache with 1-hour TTL and 30-second refetch throttling.

### 4. Downstream Ephemeral Credential Minting (O-001 Resolved)
- Inbound bearer tokens never reach downstream MCP backends (proven by live assertion in `qa/v1security` and demo E2E).
- Upon policy `ALLOW`, AgentGate mints an ephemeral RS256-signed JWT (`internal/credential`) scoped to the specific backend audience and tool call.
- Downstream credentials carry a 60-second expiration (`exp`), unique `jti`, and audited reference hash (`downstream_credential_ref`).
- Downstream MCP backends (`support-desk-mcp`) verify tokens via AgentGate's public JWKS endpoint (`/.well-known/jwks.json`) and reject duplicate tokens via an in-memory `jti` replay cache (`credverify.go`).

### 5. Tool Catalog Governance & Drift Detection
- Persistent PostgreSQL tool storage (`internal/toolcatalog`) backing `internal/toolregistry`.
- Canonical SHA-256 schema fingerprinting (`toolregistry.CanonicalSchemaFingerprint`) and drift detection (`CheckDrift`).
- Per-tool argument whitelist registry (`internal/argdecl`, O-006) preventing arbitrary parameter injection into Cedar policy evaluation.
- Dynamic tool registration, risk updates (`read`, `write`, `destructive`), and deletion via authenticated Admin REST API (`PUT/GET/DELETE /api/v1/tools/{backend_id}/{tool_name}`).

### 6. Durable & Tamper-Evident Audit Boundary (O-002 Resolved)
- PostgreSQL append-only `audit_events` persistence (`internal/audit`).
- Cryptographic SHA-256 row chaining linking each event to its predecessor (`prev_hash` + `row_hash`).
- Independent out-of-process verification via `agentgate verify-audit --workspace <id>` CLI command.
- Database trigger `trg_audit_events_immutable` actively rejecting any `UPDATE` or `DELETE` statements.
- Pre-persistence argument redaction (`redact.go`) ensuring sensitive fields are hashed, never stored in plaintext.
- Strict audit-before-ALLOW invariant: any failure to persist audit record immediately forces request `DENY`.

### 7. Database Privilege Separation
- `agentgate_migrator`: Dedicated role holding DDL schema privileges (`CREATE TABLE`, `CREATE INDEX`, `CREATE TRIGGER`) used strictly during startup migrations (`agentgate migrate`).
- `agentgate_app`: Dedicated runtime role holding strictly limited DML privileges (`SELECT`, `INSERT`, `UPDATE` on tables except `audit_events` which is `INSERT`/`SELECT` only).

### 8. Production Deployment Topology & Hardening
- Complete Docker Compose production stack in `deploy/prod/docker-compose.yml`:
  - `prod-postgres`: PostgreSQL 16 database.
  - `prod-keycloak`: Keycloak 26 OIDC server with pre-provisioned `agentgate` realm.
  - `prod-migrate`: Ephemeral container running database migrations to completion before services start.
  - `prod-agentgate`: Core governance gateway in hardened distroless container (`gcr.io/distroless/static-debian12:nonroot`).
  - `prod-agentgateway`: Envoy proxy terminating client TLS and calling AgentGate over mTLS.
  - `prod-support-desk-mcp`: Realistic demo MCP backend.
  - `prod-admin-ui`: Production React/Vite admin SPA served by unprivileged Nginx (`nginxinc/nginx-unprivileged:1.27-alpine`).
  - `prod-demo-agent`: Autonomous traffic generator.
- Production environment enforcement: zero dev default passwords or secrets permitted (`AGENTGATE_ENV=production` refuses startup if defaults are detected).

### 9. Admin UI SPA (`frontend/app/`)
- React 18, Vite, TypeScript SPA adhering to frozen contract layer (`frontend/src/`).
- Real Keycloak OIDC login with PKCE flow and token silent renew.
- Live Policy Lifecycle management (candidate creation, dry-run comparison, zero-downtime activation, rollback).
- Live Tool Catalog viewer and editor with risk level governance.
- Live Audit Events viewer with auto-refresh, cursor pagination, and decision filtering.

---

## Verification & Proof Evidence

Every claim in this document is backed by reproducible automated test suites:

| Component / Scope | Verification Command / Suite | Result | Proves |
|---|---|---|---|
| Core Go Gateway | `go test ./...` in `agentgate/` | 100% PASS (35 packages) | Decision core, audit chaining, OIDC verifier, catalog, rate limit, mTLS config, CLI subcommands |
| Demo Backend | `go test ./...` in `deploy/demo/support-desk-mcp/` | 100% PASS | In-memory ticket storage, credential signature verification, replay cache, expiry |
| Traffic Generator | `go test ./...` in `deploy/prod/demo-agent/` | 100% PASS | Scenario executor, traffic generator loop, Keycloak client_credentials auth |
| Frontend Contract | `npm test` in `frontend/` | 100% PASS (71 tests) | Contract models, state machines, API client mapping |
| Admin UI SPA | `npm test` & `npm run build` in `frontend/app/` | 100% PASS (87 tests) + dist build | React components, OIDC auth flow, tool catalog editor, audit viewer, Vite bundling |
| G6 Live Enforcement | `pwsh deploy/g6/run-e2e-matrix.ps1` in sandbox | 100% PASS (12/12 DoD scenarios) | Live agentgateway v1.4.0 proxying, ext_authz gRPC, outage fail-closed, recovery |
| Realistic Demo E2E | `pwsh deploy/demo/run-demo-e2e.ps1` in sandbox | 100% PASS (13/13 checks) | Ephemeral credential minting, inbound token secrecy, 401 direct access, catalog reclassification |
| Production Security Gate | `pwsh deploy/prod/scripts/run-security-gate.ps1` in sandbox | 100% PASS in Docker sandbox on 2026-10-09, run 3 times; GitHub CI not yet run | Clean-room deploy, OIDC RBAC, mTLS, audit chain verification, live bypass/replay/forgery/outage attacks |

---

## Architectural Decisions Summary

See [`docs/DECISIONS/OPEN_DECISIONS.md`](../DECISIONS/OPEN_DECISIONS.md):
- **Open:** O-004 (supported MCP revision — explicitly pinned to `2026-07-28`).
- **Resolved:**
  - **O-001** (downstream credential propagation — AgentGate-minted RS256 scoped JWT with 60s expiry and `jti` replay protection).
  - **O-002** (audit durability invariant — fail-closed DENY on audit failure).
  - **O-003** (agentgateway conformance boundary — verified against pinned v1.4.0).
  - **O-005** (tool identity and schema fingerprinting — canonical SHA-256 fingerprint).
  - **O-006** (argument authorization model — typed argument declaration whitelist).
  - **O-007** (AgentGate execution identity — request correlation ID persisted in audit).
  - **O-008** (ext_authz transport mapping — Envoy v3 gRPC JSON-RPC body mapping).
  - **O-009** (production UI framework — `frontend/app/` kept, `admin-ui/` consolidated).

---

## Status-update rule

Rewrite this file in place after any checkpoint transition or other meaningful state change. Do not append historical narrative here — that belongs in the relevant checkpoint's own docs. Do not claim a capability is complete until implementation and required verification have actually occurred.
