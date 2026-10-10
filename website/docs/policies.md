---
sidebar_position: 6
---

# Policies

AgentGate uses Amazon Cedar policies to evaluate access control decisions. Policies determine whether a specific agent role is allowed to execute a tool with a given risk level.

## Baseline Semantics
**Note: The default policy is a test-fixture baseline.** It is meant for development and demonstration, and must be replaced with an owner-defined policy in production. Verified by docs/OPERATIONS/RUNBOOK.md.

The baseline policy grants the following access:
- `reader`: May invoke any tool classified as `read`. Verified by agentgate/internal/fixturepolicy/fixturepolicy.go.
- `admin`: May invoke any tool classified as `read` or `write`. Verified by agentgate/internal/fixturepolicy/fixturepolicy.go.
- `payer`: May invoke a `write` tool only when a specific `amount` argument is provided and is `<= 1000`. Verified by agentgate/internal/fixturepolicy/fixturepolicy.go.
- **Any role**: Explicitly forbidden from invoking a `destructive` tool. Verified by agentgate/internal/fixturepolicy/fixturepolicy.go.

Because the baseline explicitly forbids `destructive` tools for everyone, even an `admin` cannot invoke a destructive tool out-of-the-box.

## Policy Lifecycle API
Policies are governed through an atomic candidate → dry-run → activate → rollback lifecycle. 

### 1. Create a Candidate
Create a new policy candidate. For example, to allow the `admin` role to run a destructive tool, you must modify the Cedar `forbid` rule to exclude the `admin` role, rather than just adding a `permit` rule (since a `forbid` always wins). Verified by deploy/demo/README.md.

```bash
curl -X POST http://localhost:8091/api/v1/workspaces/default/policies \
  -H "X-AgentGate-Admin-Key: agentgate-admin-secret-dev" \
  -H "Content-Type: application/json" \
  -d '{"content": "<cedar source>", "description": "Exclude admin from destructive forbid"}'
```

### 2. Activate the Policy
Once validated, activate the policy version:

```bash
curl -X POST http://localhost:8091/api/v1/workspaces/default/policies/<version>/activate \
  -H "X-AgentGate-Admin-Key: agentgate-admin-secret-dev"
```

### 3. Rollback
If you need to revert to a previous policy, you can roll back. The change reverses immediately:

```bash
curl -X POST http://localhost:8091/api/v1/workspaces/default/policies/rollback \
  -H "X-AgentGate-Admin-Key: agentgate-admin-secret-dev" \
  -H "Content-Type: application/json" \
  -d '{"target_version":"v1.0.0"}'
```

Every stage of this lifecycle is an auditable operation and securely governed. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
