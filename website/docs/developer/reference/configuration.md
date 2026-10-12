---
title: Configuration Reference
sidebar_position: 1
description: Every AGENTGATE_* environment variable, its default, and validation rules.
---

# Configuration Reference

All configuration is read from environment variables prefixed `AGENTGATE_` by
`internal/config` (`agentgate/internal/config/config.go`). Unknown variables are
ignored; required values use a non-empty default, and `Validate()` fails the
process at startup rather than allowing an undefined runtime state.

| Variable | Default | Meaning | Validation |
|---|---|---|---|
| `AGENTGATE_ENV` | `development` | Free-form deployment label (dev/staging/prod). Logging/observability context only — **not** validated against a fixed set. | — |
| `AGENTGATE_HTTP_ADDR` | `:8090` | Listen address for the HTTP surface (health, readiness, and the governance API). | must not be blank |
| `AGENTGATE_LOG_LEVEL` | `info` | Minimum log severity for the structured logger. | one of `debug`, `info`, `warn`/`warning`, `error` |
| `AGENTGATE_SHUTDOWN_TIMEOUT` | `10s` | How long graceful shutdown waits for in-flight requests before exiting. | must be a positive Go duration (`time.ParseDuration`) |
| `AGENTGATE_ADMIN_TOKEN` | `agentgate-admin-secret-dev` | Shared secret for authenticated policy-governance mutations (constant-time compared). | must not be blank |
| `AGENTGATE_DATABASE_URL` | `""` | PostgreSQL DSN for the durable policy store. **Empty → in-memory store.** | parsed/used only when non-empty |

Mock decision-core (test path only, `cmd/g1-mock-authz`):

| Variable | Default | Meaning |
|---|---|---|
| `G1_MOCK_AUTHZ_ADDR` | `:8091` | Listen address for `POST /evaluate`. |

> **Security note:** the default `AGENTGATE_ADMIN_TOKEN` and the Postgres
> credentials used in `deploy/` compose files are **development/CI fixtures
> only**. Production topologies must never embed or default credentials (the
> compose files themselves say so). If `AGENTGATE_DATABASE_URL` is left empty,
> AgentGate runs with an **in-memory** policy store — suitable for local dev/tests,
> **not** a production persistence model (audit invariants require durable
> persistence per O-002).

## How to set them on each platform

```powershell
# PowerShell
$env:AGENTGATE_ADMIN_TOKEN="dev-token"
$env:AGENTGATE_DATABASE_URL="postgres://agentgate:agentgate-dev-password@localhost:5432/agentgate_db?sslmode=disable"
cd agentgate
go run ./cmd/agentgate
```

```bash
# bash
export AGENTGATE_ADMIN_TOKEN="dev-token"
export AGENTGATE_DATABASE_URL="postgres://agentgate:agentgate-dev-password@localhost:5432/agentgate_db?sslmode=disable"
cd agentgate && go run ./cmd/agentgate
```

Negative/test configurations (invalid log level, blank address, zero
shutdown timeout, blank admin token) are pinned in
`internal/config/negative_config_test.go` — every failure class is asserted.