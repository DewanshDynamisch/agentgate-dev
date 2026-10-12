# AgentGate — Setup and Getting Started

**Audience:** anyone on the team setting up this repository locally for the first time.
**Repository state this guide matches:** post-G6 (real MCP enforcement live; G7 in progress under
the 3-team model) — see `docs/DEVELOPMENT/CURRENT_STATUS.md` for the current checkpoint, and
`docs/README.md` for full navigation. This guide covers the `agentgate/` Go module in detail
(§3-8); the `gateway/`, `frontend/`, `frontend/app/`, and `deploy/` trees each have their own
README/setup notes at their root. **Corrected 2026-09-18** — this guide previously described
`internal/authz`, `internal/identity`, and `internal/audit` as unimplemented boundary packages;
all three have been fully built since (G2, G5, G6 respectively). Read `CURRENT_STATUS.md`, not
this file, if you need the current authoritative state — this file is a setup guide, not a status
tracker, and can drift between refreshes.

---

## 1. Prerequisites

| Tool | Version | Why |
|---|---|---|
| [Go](https://go.dev/dl/) | **1.26 or newer** | Matches `agentgate/go.mod` (`go 1.26`) and `docs/TECH_STACK.md`'s "Go 1.26+" |
| Git | any recent version | Clone/branch/commit |
| A C compiler (gcc/clang/MSVC-via-mingw) | any | Only needed to run `go test -race` locally — the race detector requires cgo. Not needed to build or run the app. |

Nothing else is required yet. The module currently has **zero external dependencies** — no
Docker, database, or other service is needed to build, run, or test it at this stage.

Optional, if you want to run the same static-analysis/vulnerability checks CI runs:

- [`golangci-lint`](https://golangci-lint.run/welcome/install/)
- [`govulncheck`](https://go.dev/blog/govulncheck) — no separate install needed, see §4.

## 2. Clone the repository

```bash
git clone git@github.com:rushi-dynmsh/agentgate-dev.git
cd agentgate-dev
```

(Use the HTTPS remote instead if you have not set up an SSH key with GitHub:
`https://github.com/rushi-dynmsh/agentgate-dev.git`.)

## 3. Repository layout (what you'll see)

```
agentgate-dev/
  CLAUDE.md                  coding-agent operating rules — read before making changes
  WORKFLOW.md                the dev workflow/roles doc — read before making changes
  docs/                      canonical project documentation — start with docs/README.md
  .github/workflows/ci.yml   CI — runs the checks in §4 automatically on push/PR
  agentgate/                 the Go module — THE product code lives here
    go.mod                   module github.com/Dynamisch-LLC/agentgate, go 1.26
    cmd/agentgate/           production executable entry point: HTTP health/readiness (:8090)
                             + Envoy v3 ext_authz gRPC service (:9001, real, not a stub)
    cmd/g1-mock-authz/       non-production JSON/HTTP wrapper around the decision core,
                             used for G1 cross-stream integration — never used by cmd/agentgate
    internal/config/         typed, validated startup configuration
    internal/logging/        structured (JSON) logging
    internal/httpserver/     health/readiness HTTP endpoints, graceful shutdown
    internal/decision/       the frozen authorization decision core (Request/Result, fail-closed)
    internal/policy/         the narrow Cedar boundary — only package that imports cedar-go
    internal/fixturepolicy/  shared canonical Cedar test/dev fixture
    internal/mockauthz/      JSON wire layer for cmd/g1-mock-authz
    internal/identity/       identity claims mapping, fail-closed (G2)
    internal/toolregistry/   tool schema fingerprinting + drift detection (G2)
    internal/argdecl/        per-tool argument whitelist declarations (G2)
    internal/contextassembly/ builds decision.Request from identity+tool+args (G2)
    internal/policystore/    policy persistence, Memory + PostgreSQL (G3)
    internal/policymanager/  policy lifecycle: activation, rollback, preview (G3)
    internal/govapi/         admin governance REST API (G3)
    internal/auditevents/    policy-mutation audit events (G4)
    internal/governanceintegration/ dry-run candidate-vs-active comparison (G4)
    internal/audit/          durable, tamper-evident decision audit — Postgres, SHA-256 row
                             chaining, fail-closed on write failure (G5), real-time-integrated (G6)
    internal/authz/          real Envoy v3 ext_authz gRPC adapter/server (G6). Its E2E test
                             suite's live/unit conflation and two structurally-fake tests were
                             found and fixed in G7 Task A (2026-09-18) — see the corrective-
                             closeout addendum in docs/PHASES/G6_WORKSTREAMS/CLOSURE_SUMMARY.md §5
    qa/g1blackbox/           independent black-box suite, genuinely zero internal/* imports
    qa/g2security/, qa/g3governance/, qa/g4integration/, qa/g5audit/, qa/g6enforcement/
                             black-box in the sense of testing the contract, not internals — but
                             each imports internal/* directly (only g1blackbox is import-free)
  gateway/                   agentgateway configuration + an independent Go verification harness
  frontend/                  TypeScript models/parsing/state-machine for the frozen contract
                             (framework-agnostic by design — no rendering code; see frontend/app/)
    app/                     the production React/Vite UI (O-009, resolved 2026-09-18), consuming
                             frontend/'s contract layer via the `@contract` Vite alias
  deploy/g1/ ... deploy/g6/  one Docker Compose topology per checkpoint that needed one; g6 is the
                             current real-enforcement proof
```

All Go commands below are run **from inside `agentgate/`** — that's where `go.mod` lives, not
the repo root. `gateway/harness` is its own separate Go module (its own `go.mod`); `frontend/` and
`frontend/app/` are npm projects (Vitest and Vite respectively), not Go.

## 4. Build and test it

```bash
cd agentgate

# Formatting — must produce no output
gofmt -l .

# Static analysis
go vet ./...

# Build everything
go build ./...

# Run the test suite
go test ./...
```

Expected result: `gofmt -l .` prints nothing, and `go test ./...` reports `ok` for every package
with test files — which by now (post-G6) is most of `internal/*` and all six `qa/*` suites, not
just the original handful from G1. A small number of packages genuinely have no tests of their own
and correctly report `[no test files]`: `internal/fixturepolicy` (a shared test fixture, not code
under test), `internal/g2fixtures` (same reason), and the embedded-SQL `migrations` subdirectories
under `internal/audit/` and `internal/policystore/` (SQL files, not Go). **Corrected 2026-09-18**
— this section previously and incorrectly described `internal/authz`, `internal/identity`, and
`internal/audit` as untested boundary stubs; all three have full test suites today. If in doubt,
run `go test ./...` yourself and read the actual output rather than trusting any specific package
list in prose, here or elsewhere — that's exactly the kind of claim that goes stale.

Optional, matching CI more closely:

```bash
# Race detector — requires a C compiler (cgo). Skip if you don't have one installed;
# CI's ubuntu-latest runner always has gcc, so this still runs there either way.
go test -race ./...

# Linter (install golangci-lint first if you don't have it)
# CI uses golangci-lint v2.14.0, which supports the module's Go 1.26.9.
golangci-lint run --timeout=5m
# CI gates only new findings in each PR/push diff; a full local run may also show existing lint debt.

# Known-vulnerability scan (downloads on first run, needs network)
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

## 5. Run the application

```bash
cd agentgate
go run ./cmd/agentgate
```

By default it starts an HTTP server on `:8090` (health/readiness) **and**, since G6, a real Envoy
v3 `ext_authz` gRPC service on `:9001` — this is the actual production enforcement endpoint
`agentgateway` calls; it is not a stub or a mock. Both start from the same `go run ./cmd/agentgate`
invocation. Structured JSON logs go to stdout. In another terminal, confirm the HTTP side is
alive:

```bash
curl -i http://localhost:8090/healthz   # liveness — always 200 once the process is up
curl -i http://localhost:8090/readyz    # readiness — 200 once the listener is bound
```

The gRPC side has no plain-curl equivalent — exercise it via `deploy/g6/`'s real topology (see
`deploy/g6/README.md`) or `agentgate/internal/authz`'s own test suite, not a bare HTTP request.

**Interactive API docs:** open `http://localhost:8090/docs` in a browser for a Swagger UI you can
browse and call the governance REST API from directly ("Try it out" still needs the real admin
token — default `agentgate-admin-secret-dev`, or whatever `AGENTGATE_ADMIN_TOKEN` is set to). The
raw spec is at `http://localhost:8090/openapi.yaml`; source is
`agentgate/internal/apidocs/openapi.yaml`. Covers `internal/govapi`'s REST routes only — the
gRPC `ext_authz` enforcement boundary isn't representable in OpenAPI.

Stop it with `Ctrl+C` — it shuts down gracefully (in-flight requests are given up to the
configured shutdown timeout to finish before exit).

### Configuration

All configuration is via environment variables (prefix `AGENTGATE_`); everything has a sane
default, so none of this is required for a first run.

| Variable | Default | Meaning |
|---|---|---|
| `AGENTGATE_ENV` | `development` | Free-form deployment label, logged at startup |
| `AGENTGATE_HTTP_ADDR` | `:8090` | HTTP listen address (health/readiness) |
| `AGENTGATE_AUTHZ_GRPC_ADDR` | `:9001` | Envoy v3 `ext_authz` gRPC listen address (added G6; also settable as `AGENTGATE_AUTHZ_GRPC_PORT` for a bare port) |
| `AGENTGATE_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `AGENTGATE_SHUTDOWN_TIMEOUT` | `10s` | Max time graceful shutdown waits for in-flight requests |

Example with overrides:

```bash
AGENTGATE_HTTP_ADDR=":9090" AGENTGATE_LOG_LEVEL="debug" go run ./cmd/agentgate
```

Invalid configuration (e.g. `AGENTGATE_LOG_LEVEL=verbose`, or a non-duration
`AGENTGATE_SHUTDOWN_TIMEOUT`) causes the process to fail fast on startup with a clear error on
stderr, rather than run in an undefined state.

## 6. What "working" looks like (smoke-test checklist)

Before you start making changes, confirm your setup is good:

- [ ] `cd agentgate && gofmt -l .` prints nothing
- [ ] `go vet ./...` exits 0
- [ ] `go build ./...` exits 0
- [ ] `go test ./...` — all listed packages show `ok`
- [ ] `go run ./cmd/agentgate` logs `"starting agentgate"` then `"http server listening"`
- [ ] `curl http://localhost:8090/healthz` → `200 ok`
- [ ] `curl http://localhost:8090/readyz` → `200 ready`
- [ ] `Ctrl+C` logs a shutdown message and the process exits (not just vanishes)

If any of these fail, see §7.

## 7. Troubleshooting

- **`go: go.mod file not found`** — you're running Go commands from the repo root instead of
  `agentgate/`. `cd agentgate` first.
- **`go test -race` fails with "requires cgo"** — you don't have a C compiler on `PATH`. This
  only affects the race-detector run; plain `go test ./...` is unaffected, and CI always runs
  `-race` on Linux where gcc is preinstalled. Install a compiler (e.g. `mingw-w64` on Windows,
  `build-essential` on Debian/Ubuntu, Xcode CLI tools on macOS) only if you specifically need to
  run the race detector locally.
- **Port already in use** — something else is on `:8090`; set
  `AGENTGATE_HTTP_ADDR=":<other-port>"`.
- **`govulncheck`/`golangci-lint` not found** — they're optional locally (CI runs them on every
  PR regardless); see the install links in §1 if you want to run them yourself.

## 8. Before you push

- Read `CLAUDE.md` (repo-wide AI/engineering rules) and `/WORKFLOW.md` (repo root).
- Check `docs/DEVELOPMENT/CURRENT_STATUS.md` for the current checkpoint and
  `docs/DECISIONS/OPEN_DECISIONS.md` for unresolved architectural questions — don't guess past
  them.
- Run the checklist in §6 (CI will run the equivalent checks on your PR either way).
- `docs/PHASES/AGENTGATE_V1_3_TEAM_PARALLEL_EXECUTION_PLAN.md` describes how work is currently
  divided across the 3 teams (Backend, AI/Gateway, Frontend) and checkpoints. The specific tickets
  currently open are in `docs/PHASES/G7_WORKSTREAMS/`.

## 9. Where to go next

- `docs/README.md` — the documentation map; start here if you're not sure where something lives.
- `docs/PROJECT_DEFINITION.md` — what AgentGate is and why (read this first if you're new).
- `docs/TECH_STACK.md` — the technology choices and rationale.
- `docs/DEVELOPMENT/CI_BASELINE.md` — exactly what CI checks and why.
- `docs/PHASES/archive/REPOSITORY_BASELINE.md` — the state of the repo before any code existed
  (historical).
