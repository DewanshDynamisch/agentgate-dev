---
title: Deployment Environments
sidebar_position: 4
description: The per-checkpoint docker-compose topologies under deploy/, what is verified, and what is not.
---

# Deployment Environments

Reproducible runtime/QA topologies live under `deploy/g{N}/`, one per
checkpoint, each with a `docker-compose.yml` and a growing set of verification
helpers. **These are development/QA environments, not production topologies** —
production hardening (TLS/mTLS, secret management, no default credentials) is a
later gate and is explicitly out of scope for these files.

## Quick reference

| Dir | Services | Ports | Status / verified |
|---|---|---|---|
| `deploy/g1/` | `mock-authz` only (real); `agentgateway` + `mcp-client` are **stubs never started** (profile `gateway-stub-unavailable`) | 8091 mock; 3000/15000 stubbed gateway | ⚠️ **"written but never run"** — Docker daemon unreachable when authored; G1 smoke was direct-process instead. See `DEVOPS_G1_ENVIRONMENT.md` |
| `deploy/g2/` | `agentgate` image target + **7 negative configs** (`negative-configs/`) + `identity-mapping.yaml` + `governance-fixtures.yaml` | — | Config-fixture / security-by-example deliverable |
| `deploy/g3/` | `postgres:16-alpine` (:5432) ← `agentgate` (:8090) + optional `qa-probe` (profile `qa-manual`) | 5432 / 8090 | ✅ verified — reproducible Postgres + migration verification (`test_migrations.ps1`) |
| `deploy/g4/` | `postgres` ← `agentgate` ← `qa-probe` running `qa/g4integration` | 5432 / 8090 | ✅ the G4 governance→decision E2E topology |

## g3 — reproducible persistence (the verified Postgres path)

```bash
docker compose -f deploy/g3/docker-compose.yml up -d
curl http://localhost:8090/readyz     # "ready" (200); 503 "dependency not ready: …" if PG is down
./deploy/g3/test_migrations.ps1       # runs the migration/verification script
```

Key contact points: `agentgate` runs the PostgreSQL-backed `PostgresStore`,
auto-migrating on startup (`go:embed` schema), exposing the governance API on
`:8090`, and its `/readyz` probe fails closed (503) when the database is
unhealthy. If `AGENTGATE_DATABASE_URL` is empty the service falls back to the
in-memory store.

The optional `qa-probe` (profile `qa-manual`) runs `go test ./...` inside the
network with `AGENTGATE_TEST_POSTGRES_URL` set — this is where
`TestPostgresStore` (the store behavior suite against real SQL) and
`g3governance` run hermetically.

## g4 — integrated governance-to-decision E2E

```bash
docker compose -f deploy/g4/docker-compose.yml up -d
docker compose -f deploy/g4/docker-compose.yml --profile qa-manual up qa-probe
```

Topology: `postgres` ← `agentgate` (:8090) ← `qa-probe` running
`go test -v ./qa/g4integration/...`. The suite verifies the full lifecycle over
HTTP (candidate → validate → dry-run → activate → decision change → rollback →
decision restore).

⚠️ **Honest discrepancy:** the g4 (and g3) probe containers pass
`AGENTGATE_BASE_URL`/`AGENTGATE_ADMIN_TOKEN` in their environment, but the
`g3governance`/`g4integration` suites currently **build their own in-process
`httptest` server (memory store)** and do not read those variables. Only the
Postgres-gated `policystore` test consumes `AGENTGATE_TEST_POSTGRES_URL`. A
future "test against the running container" mode (real `AGENTGATE_BASE_URL`)
is intended but not yet implemented — do not assume the probe is exercising
the composed `agentgate` service.

## g1 — status and why

`deploy/g1/docker-compose.yml` declares the minimum G1 path
(MCP client → agentgateway → mock AgentGate) with the mock as the **only real
service**; agentgateway/mcp-client entries are stubs behind profile
`gateway-stub-unavailable` and never start. The file header records it as
**written but never run** (Docker Desktop daemon unreachable in the authoring
environment); G1's actual smoke test was a **direct-process** run documented in
`docs/PHASES/G1_WORKSTREAMS/DEVOPS_G1_ENVIRONMENT.md`. Do not present the g1
container path as verified.

The mock exposes no health endpoint (only `POST /evaluate`; everything else is
404/405) — documented as a known gap, deliberately not papered over with a
fabricated healthcheck.

## Environment variables (agentgate)

| Variable | Default | Meaning |
|---|---|---|
| `AGENTGATE_HTTP_ADDR` | `:8090` | HTTP listen address (API + health + governance) |
| `AGENTGATE_DATABASE_URL` | `""` | PostgreSQL DSN; empty → in-memory store |
| `AGENTGATE_ADMIN_TOKEN` | `agentgate-admin-secret-dev` | shared admin key for governance mutations |
| `AGENTGATE_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `AGENTGATE_ENV` | — | environment tag, e.g. `development` |
| `G1_MOCK_AUTHZ_ADDR` | `:8091` | mock listen address |

> Production topologies must **never** embed or default credentials — these
> values are dev/CI fixtures only (noted in the compose files themselves).