---
sidebar_position: 5
---

import ToolsBootstrap from './_generated/tools-bootstrap.mdx';

# Tools and Classification

AgentGate governs access to backend operations by mapping individual tool calls to risk tiers.

## Risk Levels
Each registered tool is assigned a risk level that policies can evaluate:
- `read`: Safe, non-mutating operations. Verified by docs/OPERATIONS/RUNBOOK.md.
- `write`: State-modifying operations. Verified by docs/OPERATIONS/RUNBOOK.md.
- `destructive`: Irreversible or high-risk operations. Verified by docs/OPERATIONS/RUNBOOK.md.

## Unknown Tools Denied
If an agent attempts to invoke a tool that is not registered in AgentGate's catalog, the request is immediately denied with an `unknown_tool` decision. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.

## Schema Drift Denied
To ensure that agents only call tools using an expected schema, any schema drift or unauthorized modifications are denied. Tool schema enforcement acts as an authorization boundary. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.

## Tool Registration
Tools can be registered or updated via the AgentGate Admin UI or by making an authenticated `PUT` request to the Governance API.

### Governance API
You can classify a tool by sending a `PUT /api/v1/tools/{backend}/{tool}` request. Verified by docs/OPERATIONS/RUNBOOK.md.
Below is the tools bootstrap configuration used in the demo, which demonstrates classifying tools across backends:

<ToolsBootstrap />

## Immediate Effect and Auditing
Changes to a tool's risk level take effect immediately without requiring an AgentGate restart. Verified by docs/OPERATIONS/RUNBOOK.md.
Every change to the tool catalog is durably recorded in the audit log, ensuring a tamper-evident history of classifications. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
