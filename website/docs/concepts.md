---
sidebar_position: 1
---

# Core Concepts

AgentGate introduces several core concepts to govern AI agent tool access securely.

![Request Journey](/img/request-journey.svg)

## Identity
AgentGate trusts identity only from a gateway-verified JWT, never from plain headers. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
An identity consists of:
- **Agent**: The machine principal making the request, typically identified by a `sub` claim. Verified by docs/OPERATIONS/RUNBOOK.md.
- **Roles**: Realm roles mapped from the client's service account (e.g., `reader`, `admin`), which determine access permissions. Verified by docs/OPERATIONS/RUNBOOK.md.
- **On-behalf-of (obo)**: An optional human identity that the agent is acting for. Verified by deploy/g6/probe-client/main.go.

## Tool and Risk Level
A tool is a function exposed by a backend MCP server. AgentGate maintains a runtime Tool Catalog that maps tool identities to risk tiers. Verified by docs/OPERATIONS/RUNBOOK.md.
The available risk levels are:
- `read`: Safe operations that do not mutate state (e.g., `list_tickets`). Verified by deploy/demo/README.md.
- `write`: Operations that modify state but are not critically destructive (e.g., `update_ticket_status`). Verified by deploy/demo/README.md.
- `destructive`: Irreversible or high-risk operations (e.g., `delete_ticket`). Verified by deploy/demo/README.md.

## Policy
Policies govern which roles can execute tools of a given risk level.
Policies are written in Amazon Cedar and evaluated in an atomic candidate → dry-run → activate → rollback lifecycle. Verified by docs/OPERATIONS/RUNBOOK.md.
The baseline Cedar policy grants `reader` read access, `admin` read+write access, and explicitly forbids `destructive` for every role. Verified by deploy/demo/README.md.

## Decision (ALLOW/DENY)
Every tool call is evaluated against the active policy, resulting in an ALLOW or DENY decision. Verified by deploy/demo/README.md.
Denials include reason codes, such as `policy_deny` when a policy explicitly forbids the action, or `unknown_tool` if the tool is not registered. Verified by deploy/demo/run-demo-e2e.ps1.

## Audit
AgentGate provides a durable audit trail of all policy lifecycle mutations and tool call decisions. Verified by deploy/demo/README.md.
The audit log consists of real PostgreSQL rows, which are SHA-256 hash-chained for tamper evidence. Verified by deploy/demo/README.md.
