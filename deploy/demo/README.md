# AgentGate Demonstration System — Support Desk (G7 Task B)

**What this proves that `deploy/g6` doesn't:** `deploy/g6` proves the enforcement *boundary* is
real (identity, policy, audit, fail-closed) — but its backend, `probe-mcp`, just counts calls. It
can't show what governance actually *means* for a product, because there's no product behavior to
govern. This topology (`deploy/demo`) proves the same boundary against a backend with real,
differentiated business meaning: an operator can watch a policy change turn a destructive action
from denied to allowed, for one role and not another, live.

**Additive, not a replacement.** `deploy/g6` is untouched — this is a separate compose project
(`demonet` network, ports `+1` on the gateway/agentgate/postgres side, the new backend on `9200` is internal-only, no host port; run `run-credential-e2e.ps1` for the live credential proof)
that can run alongside it.

## The scenario: a support ticketing tool

`support-desk-mcp` (`./support-desk-mcp/`) exposes three tools with deliberately mixed risk:

| Tool | Risk | What it does |
|---|---|---|
| `list_tickets` | `read` | Lists tickets, optionally filtered by status. Never mutates. |
| `update_ticket_status` | `write` | Changes an existing ticket's status. |
| `delete_ticket` | `destructive` | Permanently removes a ticket. Irreversible. |

Seeded with 3 realistic tickets on every `/_demo/reset`. The baseline Cedar policy
(`internal/fixturepolicy.CedarSource`, the same one every other checkpoint's demo/QA topology
seeds) grants `reader` read access, `admin` read+write access, and **explicitly forbids
`destructive` for every role, including admin** — so out of the box, nobody can delete a ticket.

## Run it

```bash
cd deploy/demo
docker compose up -d --build
```

Then, from `deploy/g6/probe-client` (reused as-is — it already accepts `-agent-id`/`-roles` and
mints a real JWT for whichever gateway URL you point it at):

```bash
# Reader can list tickets (read) — ALLOW
go run . -url http://localhost:3001 -tool list_tickets -skip-init -agent-id agent-reader -roles reader

# Admin can update a ticket (write) — ALLOW
go run . -url http://localhost:3001 -tool update_ticket_status \
  -args '{"ticket_id":"TCK-1001","new_status":"resolved"}' -skip-init -agent-id agent-admin -roles admin

# Even admin cannot delete a ticket (destructive) under the baseline policy — DENY
go run . -url http://localhost:3001 -tool delete_ticket \
  -args '{"ticket_id":"TCK-1001"}' -skip-init -agent-id agent-admin -roles admin
```

## The governance story: watch a real policy change flip real behavior

This is the part that's the actual point — not a hardcoded fixture, a real
candidate → activate → rollback cycle through the real governance API
(`internal/govapi`), verified live 2026-09-21:

**1. Create a candidate that scopes the destructive-forbid to exclude admin**, instead of
forbidding it unconditionally (Cedar `forbid` always wins over `permit`, so this has to modify the
forbid clause itself, not just add a new permit — see the policy content in the commit, or fetch
it via `GET /api/v1/workspaces/default/policies`):

```bash
curl -X POST http://localhost:8091/api/v1/workspaces/default/policies \
  -H "X-AgentGate-Admin-Key: agentgate-admin-secret-dev" -H "Content-Type: application/json" \
  -d @candidate.json   # candidate.json: {"content": "<cedar source>", "description": "..."}
```

**2. Activate it:**

```bash
curl -X POST http://localhost:8091/api/v1/workspaces/default/policies/<version>/activate \
  -H "X-AgentGate-Admin-Key: agentgate-admin-secret-dev"
```

**3. The exact same `delete_ticket` call that was just denied now succeeds — for admin only:**

```bash
go run . -url http://localhost:3001 -tool delete_ticket \
  -args '{"ticket_id":"TCK-1001"}' -skip-init -agent-id agent-admin -roles admin
# Tool Call Succeeded: "Ticket TCK-1001 permanently deleted"

go run . -url http://localhost:3001 -tool delete_ticket \
  -args '{"ticket_id":"TCK-1002"}' -skip-init -agent-id agent-reader -roles reader
# still DENY — the policy is scoped to admin, not a blanket unlock
```

**4. Roll back, and the change reverses immediately:**

```bash
curl -X POST http://localhost:8091/api/v1/workspaces/default/policies/rollback \
  -H "X-AgentGate-Admin-Key: agentgate-admin-secret-dev" -H "Content-Type: application/json" \
  -d '{"target_version":"v1.0.0"}'
# delete_ticket as admin now denies again
```

**Full durable audit trail of exactly this run** (real PostgreSQL rows, SHA-256 hash-chained, not
edited): `demo_scenario_audit_trail.txt`. Twelve rows: 2 policy-lifecycle mutations (candidate,
activate), 4 real tool-call decisions under the baseline, another 2 mutations (candidate, activate
for the destructive-permit version), 2 more tool-call decisions showing the changed behavior, a
rollback mutation, and one final tool-call decision proving the rollback took effect.

## Credentials

Uses a stub credential in the meantime — see `CREDENTIAL_REQUIREMENTS.md` for what a real
deployment would need, handed to Backend for their G7 Task B implementation.

## Follow-up: real credentials

Once Backend's real `credential.Issuer` implementation exists (currently only the interface is
designed — `agentgate/internal/credential/credential.go`), swap it in here in place of the stub.
Tracked as a follow-up note per `docs/PHASES/G7_WORKSTREAMS/02_AI_GATEWAY_G7.md` §3 DoD item 5 —
not a new ticket.

## Tear down

```bash
docker compose down -v
```
