---
sidebar_position: 2
---

# Quickstart

Get AgentGate running locally with a realistic demonstration system in under 15 minutes.

## Prerequisites
- Docker
- Go ≥ 1.24 (for the token helper)
Verified by deploy/demo/README.md and deploy/g6/probe-client/main.go.

## 1. Start the Demo Stack

First, generate the demo credential key needed by AgentGate to mint downstream credentials. Verified by deploy/demo/run-demo-e2e.ps1.

```bash verify
mkdir -p deploy/demo/secrets
openssl genpkey -algorithm RSA -out deploy/demo/secrets/cred_key.pem -pkeyopt rsa_keygen_bits:2048
```

Then, start the Docker Compose stack. This will build and run AgentGate, a Postgres database, an API Gateway, and a sample support-desk backend. Verified by deploy/demo/docker-compose.yml.

```bash verify
cd deploy/demo
docker compose up -d --build
```

Wait for the services to become healthy. Verified by deploy/demo/docker-compose.yml.

```bash verify
sleep 15
```

## 2. Invoke Tools

Use the probe-client to simulate an agent calling tools through the gateway. The client automatically mints an inbound token. Verified by deploy/g6/probe-client/main.go.

First, list the tickets. The `reader` role is allowed to perform `read` operations like `list_tickets`. Verified by deploy/demo/README.md.

```bash verify
cd ../g6/probe-client
go run . -url http://localhost:3001 -tool list_tickets -skip-init -agent-id agent-reader -roles reader
```
```text expect
Tool Call Succeeded:
```

Now, attempt to delete a ticket. The `reader` role is forbidden from performing `destructive` operations like `delete_ticket`. Verified by deploy/demo/README.md.

```bash verify
go run . -url http://localhost:3001 -tool delete_ticket -args '{"ticket_id":"TCK-1001"}' -skip-init -agent-id agent-reader -roles reader || true
```
```text expect
tool call failed
```

## 3. View the Audit Log

Every decision is cryptographically logged. Inspect the audit rows in the Postgres database. Verified by deploy/demo/README.md.

```bash verify
cd ../../demo
docker compose exec -T postgres psql -U agentgate -d agentgate_demo_db -At -c "SELECT decision, reason FROM audit_events ORDER BY sequence_number DESC LIMIT 5"
```
```text expect
deny|policy_deny
allow|
```

## 4. Tear Down

Clean up the demonstration environment. Verified by deploy/demo/README.md.

```bash verify
docker compose down -v
```

:::info What you just saw
You started AgentGate, issued an allowed request and a denied request based on role and tool risk, and viewed the immutable audit log. Read the [Core Concepts](./concepts.md) for a deeper understanding.
:::
