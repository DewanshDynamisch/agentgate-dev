---
title: System Overview
sidebar_position: 1
description: The end-to-end AgentGate request path, component ownership, and the deployment trust topology.
---

# System Overview

## The intended production path

AgentGate is an in-path authorization decision point sitting behind
[agentgateway](https://agentgateway.dev) (the MCP data-plane proxy). The
division of labour is the load-bearing design constraint: **agentgateway owns
the MCP transport; AgentGate owns identity resolution, policy, and audit; the
tool/backend owns per-record authorization.**

```mermaid
flowchart LR
    A["AI agent / MCP client<br/>(unmodified, any harness)"]
    GW["agentgateway (off-the-shelf, Rust)<br/>MCP transport/routing • tool discovery<br/>JWT validation • ext_authz callout"]
    AG["AgentGate (our product, Go)<br/>identity → tool governance →<br/>Cedar decision → audit"]
    B["Real MCP backend/tool<br/>(owns per-record authorization)"]

    A -- "MCP over HTTP + Bearer JWT" --> GW
    GW -- "ext_authz callout: allow or deny? + validated claims" --> AG
    AG -- "allow/deny + exact policy version" --> GW
    GW -- "only on ALLOW: forwards the tool call" --> B
```

- **agentgateway** is the only externally-reachable component. It validates the
  inbound JWT (`jwtAuth`, strict mode) and calls AgentGate **before** forwarding
  a governed tool call.
- **AgentGate** never speaks MCP and is not a public endpoint. Its decision
  surface and admin API are internal-network-only
  ([PRODUCTION-INVARIANTS §9](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/SECURITY/PRODUCTION-INVARIANTS.md)).
- **Robustness consequence:** AgentGate down = governed calls denied. Fail-closed
  is a deliberate choice, not an accident.

## What exists today vs. the target

The current codebase (checkpoint G5, see
[Current Status](../development/current-status.md)) implements the **AgentGate
side** of this diagram and proves its contract with a JSON/HTTP mock alongside a durable append-only Postgres audit boundary (`internal/audit`). The real
`ext_authz` gRPC boundary (`internal/authz`) is *not built yet* — it is a
documented placeholder. Today's verified flows:

```mermaid
flowchart LR
    subgraph Today
        CLI["curl / harness / any HTTP caller"]
        MOCK["cmd/g1-mock-authz — mock transport<br/>(internal/mockauthz)<br/>wraps the REAL decision core"]
        CORE["decision core<br/>(internal/decision + internal/policy + Cedar)"]
        GOV["governance API<br/>(internal/govapi)<br/>policy lifecycle + dry-run"]
        AUD["durable audit<br/>(internal/audit)<br/>hash chaining + redaction"]

        CLI --> MOCK --> CORE --> AUD
        CLI --> GOV
    end
    PG[("Postgres<br/>(policy store + audit_events)")]
    GOV --> PG
    AUD --> PG
```

The product's decision logic is real; the *transport* to it in production
(gRPC ext_authz) is the remaining work. The mock is explicitly temporary
(execution-plan Rule 2: mocks are parallelization tools, never permanent
substitutes).

## Backend layering (how the Go module is organized)

The `agentgate` module is deliberately layered so only one package ever touches
the Cedar SDK:

```mermaid
flowchart TD
    EXT["cmd/agentgate — production entry point"]
    MOCK_BIN["cmd/g1-mock-authz — test-only mock binary"]
    GOVAPI["internal/govapi — admin REST API"]
    INTEG["internal/governanceintegration — dry-run compare"]
    MGR["internal/policymanager — lifecycle + active-engine cache"]
    STORE["internal/policystore — Postgres + memory"]
    ASM["internal/contextassembly"]
    ARG["internal/argdecl"]
    ID["internal/identity"]
    TR["internal/toolregistry"]
    DEC["internal/decision — decision core"]
    POL["internal/policy — the ONLY Cedar boundary"]
    CEDAR(["cedar-go SDK"])

    EXT --> GOVAPI
    EXT --> MGR
    MGR --> STORE
    GOVAPI --> MGR
    GOVAPI --> INTEG
    INTEG --> MGR
    MGR --> DEC
    ASM --> ARG
    ASM --> ID
    ASM --> TR
    ASM --> DEC
    DEC --> POL
    POL --> CEDAR
```

One-package Cedar confinement is a security design, not an accident: policy
types that cross into `internal/decision` are pure-Go value types, and the Wire
contract is JSON — nothing Cedar-specific leaks outward.

## Trust model in one breath

AgentGate trusts the identity fields on a decision request **structurally** —
the only legitimate production caller is the future `internal/authz` layer,
which will populate them from claims that agentgateway already
cryptographically validated. Every other path (including the mock) is an
unauthenticated test/integration path by definition. This is documented in the
[G1 contract](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md)
and enforced by fail-closed rules at every layer.

## Next

- [Components](./components.md) — a package-by-package map of the Go module.
- [Security model](./security-model.md) — the binding invariants.
- [Gateway integration](./gateway-integration.md) — agentgateway config and the contract-mismatch finding (open decision O-008).