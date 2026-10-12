---
title: Local Setup
sidebar_position: 3
description: Clone, build, run, and smoke-test the AgentGate Go service locally.
---

# Local Setup

This page walks you through a first checkout, build, test, and run of the
AgentGate Go service (`agentgate/`). The repository's own
[`docs/DEVELOPMENT/SETUP.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DEVELOPMENT/SETUP.md)
is the canonical, always-maintained version of this guide — consult it when this
page lags.

## 1. Clone

```bash
git clone git@github.com:rushi-dynmsh/agentgate-dev.git   # or use the HTTPS URL below
cd agentgate-dev
```

HTTPS alternative: `https://github.com/rushi-dynmsh/agentgate-dev.git`.

## 2. Build and test the Go module

```bash
cd agentgate                   # this is where go.mod lives

gofmt -l .                     # must print nothing
go vet ./...
go build ./...
go test ./...                  # full unit suite + in-package integration tests
```

The packages with test coverage include `internal/config`, `internal/logging`,
`internal/httpserver`, `internal/decision`, `internal/policy`,
`internal/identity`, `internal/toolregistry`, `internal/argdecl`,
`internal/contextassembly`, `internal/policystore` (memory impl always;
Postgres tests are skipped unless `AGENTGATE_TEST_POSTGRES_URL` is set),
`internal/policymanager`, `internal/govapi`,
`internal/governanceintegration`, `internal/mockauthz`, and
`internal/auditevents`. Boundary-only packages (`internal/authz`,
`internal/audit`) report `[no test files]` — expected, not a failure.

Optional, closer to CI:

```bash
go test -race ./...                 # needs a C compiler (cgo)
golangci-lint run --timeout=5m
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

## 3. Run the production service

```bash
go run ./cmd/agentgate
```

The service starts an HTTP listener on `:8090` and emits structured JSON logs.
In another terminal:

```bash
curl -i http://localhost:8090/healthz    # liveness — 200 "ok" once up
curl -i http://localhost:8090/readyz     # readiness — 200 "ready" (DB-aware when configured)
```

Stop it with `Ctrl+C` — graceful shutdown with a bounded drain window.

### Run it with Postgres (optional)

The default in-memory policy store is enough for a first run. To exercise the
real persistence lifecycle, either set `AGENTGATE_DATABASE_URL` to a running
Postgres (the service auto-migrates on startup), or use the compose stack:

```bash
cd deploy/g3
docker compose up -d          # starts postgres + agentgate
curl http://localhost:8090/readyz   # 200 "ready"
docker compose down           # optional: down -v drops the data volume
```

See [Deployment environments](../development/deployment-environments.md) for
the g3/g4 runbooks and the test^migrations script.

## 4. The G1 contract mock (separate, non-production binary)

Many QA suites and the gateway harness talk to the decision core through a
JSON/HTTP mock that wraps the **real** decision engine:

```bash
cd agentgate
go run ./cmd/g1-mock-authz                          # listens on :8091
# G1_MOCK_AUTHZ_ADDR=":9000" go run ./cmd/g1-mock-authz   # to change the port
```

It exposes only `POST /evaluate` (see [Wire contract](../reference/wire-contract.md)).
It is **never** started from the production path (`cmd/agentgate` ignores it).

## 5. Governance API smoke test

With `cmd/agentgate` running (dev default admin token
`agentgate-admin-secret-dev`):

```bash
# Health
curl http://localhost:8090/readyz

# Policy lifecycle over the admin-authenticated governance API
curl -s http://localhost:8090/api/v1/workspaces/dev/policies \
  -H "Authorization: Bearer agentgate-admin-secret-dev"
# -> {"policies":[]} on a fresh in-memory store
```

Every `/api/v1/...` endpoint requires the admin key — via the
`X-AgentGate-Admin-Key` header or `Authorization: Bearer <token>`.
Unauthenticated calls get `401`. Full endpoint list:
[API reference](../reference/api-reference.md).

## 6. Frontend and gateway harness (independent projects)

Each has its own toolchain; neither is needed for the Go service.

```bash
# frontend — TypeScript contract layer (typecheck + tests)
cd frontend
npm install
npm run typecheck
npm test

# gateway harness — independent Go module proving the wire contract against the mock
cd gateway/harness
go test ./... -v              # skips (with instructions) if the mock is not running
```

## Smoke-test checklist

- [ ] `cd agentgate && gofmt -l .` prints nothing
- [ ] `go vet ./...` and `go build ./...` exit 0
- [ ] `go test ./...` — all listed packages show `ok`
- [ ] `go run ./cmd/agentgate` logs `starting agentgate` then `http server listening`
- [ ] `curl http://localhost:8090/healthz` → `200 ok`
- [ ] `curl http://localhost:8090/readyz` → `200 ready`
- [ ] `Ctrl+C` logs a shutdown message and exits

## Troubleshooting

- **`go: go.mod file not found`** — you're in the repo root, not `agentgate/`. `cd agentgate`.
- **`go test -race` says "requires cgo"** — no C compiler on `PATH`. Only affects the race run; CI still runs `-race` on Linux.
- **Port already in use** — set `AGENTGATE_HTTP_ADDR=":<other-port>"`.
- **`AGENTGATE_*` config** — see [Configuration reference](../reference/configuration.md).

## Before you change code

1. Read [`CLAUDE.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/CLAUDE.md) and [`WORKFLOW.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/WORKFLOW.md).
2. Check [`docs/DEVELOPMENT/CURRENT_STATUS.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DEVELOPMENT/CURRENT_STATUS.md) and [`docs/DECISIONS/OPEN_DECISIONS.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DECISIONS/OPEN_DECISIONS.md) — don't guess past open questions.
3. Keep changes scoped to your task; the codebase has explicit rules about that.