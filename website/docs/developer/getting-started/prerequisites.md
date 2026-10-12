---
title: Prerequisites
sidebar_position: 2
description: What you need installed to build, run, and test the AgentGate monorepo.
---

# Prerequisites

Only two tools are required to build and run the product: **Go** and **Git**.
The frontend and the docs site need Node.js; Docker is only needed for the
deployment environments.

## Required

| Tool | Version | Why |
|---|---|---|
| [Go](https://go.dev/dl/) | **1.26 or newer** | Matches `agentgate/go.mod` (`go 1.26`). |
| [Git](https://git-scm.com/) | any recent | Clone/branch/commit. |

That is genuinely enough to build, run, and test the entire Go service. There
are **no** runtime services required at this stage — no Postgres, no
agentgateway, no Docker — unless you specifically opt into them (see below).

## Optional

| Tool | Version | When you need it |
|---|---|---|
| A C compiler (gcc/clang/mingw) | any | Only to run `go test -race` locally — the race detector requires cgo. CI runs it on Linux where gcc is preinstalled. |
| [golangci-lint](https://golangci-lint.run/welcome/install/) | any | To run the same lint check CI runs. |
| Node.js + npm | Node ≥ 20 | The `frontend/` TypeScript contract layer (`npm test`, `npm run typecheck`) and the `developer-docs/` Docusaurus site. |
| [Docker](https://docs.docker.com/get-docker/) | any recent | The `deploy/g1`–`g4` environments (Postgres, containerized service, QA probes). |
| [agentgateway](https://agentgateway.dev) binary/image | latest | Real-gateway verification under `gateway/` (optional; the harness runs without it). |

## Notes

- **Postgres-backed features are opt-in.** The service defaults to an
  in-memory policy store; set `AGENTGATE_DATABASE_URL` (or use the
  `deploy/g3`/`g4` compose stacks) to exercise the Postgres `policystore`.
  This is configured so the local developer experience needs nothing but Go.
- **The gateway harness is a separate Go module** (`gateway/harness` — its own
  `go.mod`), and `frontend/` is an npm/Vitest project — they are not part of the
  `agentgate` module and have their own commands.

:::tip Working directory matters
All `go` commands for the product run **from inside `agentgate/`**, where
`go.mod` lives — not from the repo root. The developer-docs lives at `developer-docs/`;
the canonical docs at `docs/`. See [Repository structure](../repository-guide/structure.md).
:::

Now follow **[Local setup](./local-setup.md)**.