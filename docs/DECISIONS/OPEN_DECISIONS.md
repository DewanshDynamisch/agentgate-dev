# AgentGate — Open Decisions

**Status:** Active — O-004 open; O-001, O-002, O-003, O-005, O-006, O-007, O-008, O-009 resolved
**Date:** 2026-10-09

This file contains unresolved questions that may affect architecture or implementation.

An open decision is not an invitation for coding agents to guess.

## O-004 — Supported MCP revision(s)

**Priority:** High

The current Technology Stack Plan targets MCP `2026-07-28`, while compatibility with older deployed clients remains unresolved. AgentGate v1 explicitly pins and supports MCP revision `2026-07-28`.

**Current action:** Stays open with pinned revision `2026-07-28` for multi-version matrix evaluation post-v1.

## Resolved

### O-001 — Downstream identity / credential propagation (resolved 2026-09-29, v1 production)

**Priority was:** Critical

**Resolution:** AgentGate-minted RS256, per-call scoped JWT, jti replay, 60s lifetime, audit ref.
AgentGate does not forward the caller's inbound bearer token. Instead, upon policy `ALLOW`, AgentGate mints an ephemeral RS256-signed JWT (`internal/credential`) scoped to the specific backend audience and tool call with a 60-second lifetime (`exp`), unique `jti`, and records the SHA-256 reference hash in the immutable audit log (`downstream_credential_ref`). Downstream MCP backends verify signatures via public JWKS (`/.well-known/jwks.json`), enforce audience/expiry, and reject duplicate tokens via an in-memory `jti` replay cache (`support-desk-mcp/credverify.go`).
Proven by `internal/credential` unit tests, `deploy/demo/support-desk-mcp/credverify.go`, and `agentgate/qa/v1security/security_gate_test.go` Scenarios 01-05 (credential propagation, signature verification, replay rejection, expiry rejection, audience mismatch rejection).

### O-009 — Production UI framework disposition (resolved 2026-09-18, by consolidation)

**Priority was:** Medium

**Original question:** `frontend/src` deliberately left the production UI framework unchosen. A
teammate independently built `admin-ui/` (React/Vite, PR #3, 2026-09-18 morning) against that
contract layer. Hours later, a second teammate — working from the G7 Frontend ticket
(`docs/PHASES/G7_WORKSTREAMS/03_FRONTEND_G7.md`) but apparently without pulling latest
`development` first — independently built a second, separate app at `frontend/app/` (PR #5/#6,
2026-09-18 evening), whose own doc stated "there is no separate `admin-ui/` directory in this
checkout." Two competing implementations existed simultaneously in `development` — the exact
silent-drift risk this decision was recorded to prevent.

**Resolution:** `admin-ui/` removed; `frontend/app/` is the production UI going forward. Rationale:
`frontend/app/` already had committed automated test coverage (`ActivatePolicyPage.test.tsx`,
`PoliciesListPage.test.tsx`, `decision-fixtures.test.ts`, `policy-workflow.test.ts`) and its own
`PROPOSED_G8_API_CONTRACTS.md` — both further along than `admin-ui/` was at the point of decision.
Not a judgment that `admin-ui/`'s work was lower quality; it was a straightforward "one of these
has to go" call made in favor of the more-progressed option. See
`docs/PHASES/G7_WORKSTREAMS/03_FRONTEND_G7.md` for the corrected, `frontend/app/`-targeted ticket.

### O-008 — ext_authz transport mapping to decision.Request contract (resolved 2026-09-16, G6 Phase 1)

**Priority was:** High

**Resolution:** Verified empirically against pinned `agentgateway:v1.4.0` (`sha256:771afaf093065477fa296eb90dcb618a0300165f12a32f80bbdd1427fab900ec`).
1. agentgateway uses Envoy v3 gRPC `envoy.service.auth.v3.Authorization/Check` callout configured via `policies.extAuthz.protocol.grpc: {}`.
2. Raw MCP JSON-RPC 2.0 tool call body (`tools/call`, tool name, arguments) is delivered in `CheckRequest.Attributes.Request.Http.Body` and `RawBody` when `includeRequestBody: { maxRequestBytes: 1048576, allowPartialMessage: false, packAsBytes: false }` is enabled.
3. Authenticated JWT identity is delivered in `CheckRequest.Attributes.MetadataContext.FilterMetadata["envoy.filters.http.jwt_authn"]`.
4. Fail-closed behavior is verified empirically: `DENY` -> 0 backend calls, `MALFORMED` -> 0 backend calls, `UNAVAILABLE` -> 0 backend calls.
5. The production `internal/authz` adapter unmarshals the JSON-RPC body, verifies tool governance against `toolregistry` and `argdecl`, and calls `audit.AuditedDecisionService.Evaluate()`. See `docs/PHASES/G6_WORKSTREAMS/G6_GATEWAY_CONTRACT.md`.

### O-003 — agentgateway conformance/security boundary (resolved 2026-09-16, G6 checkpoint)

**Priority was:** High

**Resolution:** Verified empirically through independent black-box E2E security suite (`agentgate/qa/g6enforcement`) running against pinned `agentgateway:v1.4.0` in the integrated Docker topology (`deploy/g6/docker-compose.yml`).
1. Conformance proved across 12 mandatory DoD scenarios (100% PASS).
2. Exactly 1 backend call occurred for authenticated, authorized requests (`read_status`).
3. Exactly 0 backend calls occurred across all 11 failure/denial/outage scenarios (unknown tool, denied tool, missing identity, ambiguous identity, malformed JSON, schema drift, client metadata spoofing, oversized payload, live AgentGate outage, policy evaluation fault).
4. Live service recovery was verified (1 backend call after AgentGate restart).
5. Zero client trust: client cannot bypass enforcement via injected headers or body parameters.
6. See `docs/PHASES/G6_WORKSTREAMS/results/QA_SECURITY_G6_REPORT.md` and `docs/PHASES/G6_WORKSTREAMS/CLOSURE_SUMMARY.md`.

### O-002 — Audit durability invariant (resolved 2026-09-14, G5 checkpoint)

**Priority was:** Critical

**Original question:** whether an ALLOW may be returned when the corresponding audit event has not yet been durably persisted, given the technology plan's asynchronous audit-write concept versus an architecture review recommending against committing ALLOW without durable audit persistence.

**Resolution:** no — an ALLOW must not be returned unless a durable audit outcome for that exact decision is guaranteed. Audit-durability failure fails the request closed (`DENY`). Recorded as a binding invariant in `docs/SECURITY/PRODUCTION-INVARIANTS.md §5` and §12.2.

**Implementation (Completed G5, 2026-09-14):** Implemented in `agentgate/internal/audit`: append-only `audit_events` PostgreSQL persistence, tamper-evident SHA-256 row chaining (`prev_hash` + `row_hash`), independent out-of-process `ChainVerifier`, pre-persistence argument redaction (`redact.go`), fail-closed decision enforcement (`service.go`), DB immutability triggers (`prevent_audit_modification`), and DB privilege separation (`agentgate_app` vs `agentgate_migrator`). Formally approved by the Lead Architect 2026-09-14.

### O-006 — Argument authorization model (resolved 2026-09-13, G2 checkpoint)

**Priority was:** High

**Original question:** how to expose relevant tool arguments to policy evaluation without turning
arbitrary tool input into an uncontrolled policy surface.

**Resolution:** per-tool typed declaration registry (`internal/argdecl`). Arbitrary argument
passthrough was rejected in favor of an explicit whitelist model:

- Each tool declares its policy-visible arguments via a `DeclarationSet` specifying name, type
  (`string`, `int64`, `bool`), and required/optional status.
- Only declared arguments are extracted and resolved into `decision.Request.Arguments`.
- **Undeclared arguments** are excluded from policy input — they are structurally invisible to Cedar.
- **Explicit JSON `null`** is rejected (not coerced to a zero value), preventing type-confusion bypasses.
- **Missing required arguments** fail closed (request denied).
- **Omitted optional arguments** are absent rather than fabricated with defaults.
- Resolution produces deterministic typed `AttributeValue` entries that feed the frozen G1
  `decision.Request` contract.

**Why arbitrary passthrough was rejected:** passing raw MCP `arguments` wholesale into Cedar
would create an uncontrolled policy surface — any new or renamed tool argument would silently
become a policy input without governance review. The declaration registry ensures that only
explicitly approved attributes influence authorization decisions.

**Implementation location:** `agentgate/internal/argdecl/` (declaration types, resolution logic,
and comprehensive tests). Context assembly adapter at `agentgate/internal/contextassembly/`
populates `decision.Request.Arguments` from resolved declarations.

**Scope note:** closing O-006 resolves the *policy-input exposure model* — which arguments become
policy-visible and how. It does not mean argument-level authorization is a fully deployed business
authorization system; Cedar policy semantics determine the eventual authorization decisions
once the declared arguments reach policy evaluation.

**Affected architecture documents:**
- `docs/SECURITY/PRODUCTION-INVARIANTS.md` §6 (argument-authorization boundary invariant)
- `docs/PHASES/G2_WORKSTREAMS/G2_CLOSURE_SUMMARY.md` §1 (W1), §4 (O-006 status)
- `docs/PHASES/G2_WORKSTREAMS/results/GO_BACKEND_G2_REPORT.md` §Open Decisions
- `agentgate/internal/decision/types.go` (AttributeValue type, historical O-006 comment)
- `agentgate/internal/decision/doc.go` (historical O-006 reference)

### O-005 — Tool identity and schema fingerprint (resolved 2026-09-13, G2 checkpoint)

**Priority was:** High

**Resolution:** Implemented in `agentgate/internal/toolregistry`. Defines deterministic canonicalization and SHA-256 fingerprinting for backend identity + tool name + schema. Any schema drift triggers `CheckDrift()` detection and deny-by-default behavior until classified.

### O-007 — AgentGate execution identity (resolved 2026-09-13, G2 & G5 checkpoints)

**Priority was:** Medium/High

**Resolution:** Defined `execution_id` correlation identifier populated across `decision.Request` (`internal/contextassembly`) and persisted in `audit_events` (`internal/audit`) for end-to-end request tracing and audit correlation.

## Decision protocol

For each decision:

1. state the question;
2. identify security/product/implementation impact;
3. verify external facts where necessary;
4. choose a solution;
5. record the rationale;
6. update affected architecture/plan documents;
7. only then remove the item from this file.

Open decisions must not be silently resolved in code.
