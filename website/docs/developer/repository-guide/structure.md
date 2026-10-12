---
title: Repository Structure
sidebar_position: 1
description: Where everything lives in the agentgate-dev monorepo.
---

# Repository Structure

```text
agentgate-dev/
├── CLAUDE.md                  coding-agent operating rules — read before changing code
├── WORKFLOW.md                the checkpoint workflow (roles, loops, documentation rule)
├── README.md                  repo landing page; links to the canonical docs
├── docs/                      CANONICAL project documentation (see next page)
├── .github/workflows/ci.yml   CI: gofmt/vet/test-race/lint/vulncheck
│
├── agentgate/                 the Go module — THE product code
│   ├── go.mod                 module github.com/Dynamisch-LLC/agentgate, go 1.26
│   ├── cmd/agentgate/         production entry point
│   ├── cmd/g1-mock-authz/     non-production decision-core mock (test/QA path)
│   ├── internal/              config, logging, httpserver, decision, policy,
│   │                          identity, toolregistry, argdecl, contextassembly,
│   │                          policystore, policymanager, govapi,
│   │                          governanceintegration, auditevents, mockauthz,
│   │                          fixturepolicy, authz (placeholder), audit (placeholder)
│   └── qa/                    independent QA suites (g1blackbox, g2security,
│                              g3governance, g4integration)
│
├── gateway/                   agentgateway config + independent verification harness
│   ├── config/                g1-agentgateway.yaml + dev JWKS fixture
│   ├── fixtures/              wire fixtures (8 root + g2/)
│   ├── harness/               separate Go module (own go.mod)
│   └── docs/                  G2 evidence + G6 handoff
│
├── frontend/                  TypeScript contract layer (models, client, state, views)
│   ├── src/{api,models,parsing,state,view,fixtures}/
│   └── test/                  Vitest suites
│
├── deploy/                    per-checkpoint reproducible environments
│   ├── g1/  (contract-test topology for the mock)
│   ├── g2/  (config fixtures, negative configs, agentgate image target)
│   ├── g3/  (reproducible Postgres topology + migration script)
│   └── g4/  (integrated governance-to-decision E2E topology)
│
└── developer-docs/                 THIS Docusaurus developer-doc site
    ├── docs/                  site content
    ├── src/                   landing page + styling
    ├── static/                static assets
    └── artefacts/             research notes, generator scripts, generated data
```

## Working-directory rules (common mistakes)

- **Go commands run inside `agentgate/`**, not the repo root — `go.mod` lives
  there. Exception: `gateway/harness` is its own module (run from `gateway/harness/`).
- **`frontend/` is npm/Vitest**, not Go — use `npm install`, `npm test`,
  `npm run typecheck`.
- **`developer-docs/` is npm/Docusaurus** — use `npm install`, `npm run build`,
  `npm run start`.
- **The repo `docs/` tree is the canonical documentation**, not this site.
- `docs/prompts/`, `docs/.../results/`, and `developer-docs/node_modules` are
  gitignored working material — committed docs are the durable records.