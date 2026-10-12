---
title: CI
sidebar_position: 5
description: What GitHub Actions checks on every push/PR and what is intentionally deferred.
---

# CI

Single GitHub Actions workflow (`.github/workflows/ci.yml`) — **validation only**,
not a deployment pipeline. Triggers on every push to `main`/`master` and every
pull request; a `concurrency` group cancels stale runs on the same ref, and
`permissions: contents: read` is minimal privilege. See the canonical
[`docs/DEVELOPMENT/CI_BASELINE.md`](https://github.com/rushi-dynmsh/agentgate-dev/blob/main/docs/DEVELOPMENT/CI_BASELINE.md).

## Go job (`agentgate/`)

Two design rules:

1. **Gated on `go.mod` existing** — the workflow detects the module first; if it
   is absent it logs a notice and finishes green (there is nothing to validate).
2. **No graceful skipping once it exists** — a present module means every check
   runs unconditionally and must pass. There is no path where a broken check is
   silently skipped.

In order:

| Check | Reason |
|---|---|
| `gofmt -l .` | cheapest formatting signal, runs first; fails on any unformatted file |
| `go vet ./...` | static analysis for common correctness mistakes |
| `go test -race ./...` | full suite, race detector on (agentgate is concurrency-heavy; race = default requirement) |
| `golangci-lint` | default rule set (no project `.golangci.yml` yet) |
| `govulncheck` | known-vulnerability scan against the actual module dependency graph |

The Go toolchain is pinned from `agentgate/go.mod` (`go-version-file`, `check-latest`)
so CI always matches the module's declared Go version.

## Frontend job (`frontend/`)

Gated on `package.json` existing, then: `npm ci` (frozen lockfile) →
`npm run typecheck` (strict tsc) → `npm test` (Vitest), on Node 20.

## What CI does NOT run (deliberately)

- **Postgres-gated tests / external-service suites** — those need live resources
  (see `AGENTGATE_TEST_POSTGRES_URL` in [Testing](./testing.md)) and are run
  manually against the deploy topologies.
- **Docker builds, SBOM (CycloneDX), code signing, deployment/release
  workflows, testcontainers/fuzzing** — explicitly deferred phases in
  `CI_BASELINE.md`. They are absent, not disabled.

## Local equivalent

Run the same five Go checks locally:

```bash
cd agentgate
gofmt -l . && go vet ./... && go test -race ./... && golangci-lint run --timeout=5m && go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```