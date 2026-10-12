# AgentGate — Developer Docs Site

> **Deprecated.** This content now lives in the unified documentation site at
> [`website/docs/developer/`](../website/docs/developer/) and is served under the
> **Architecture & Contributor Guide** navbar entry (`cd website && npm start`).
> Edit the pages there, not here. This folder is kept until the merge is reviewed.
> Note: `artefacts/scripts/generate-commit-index.mjs` still writes to
> `developer-docs/docs/history/commit-index.md`; copy the regenerated file to
> `website/docs/developer/history/` until the script is repointed.

This folder is a [Docusaurus](https://docusaurus.io/) static site that is part of
the `agentgate-dev` monorepo. It is the synthesized, source-checked developer
knowledge base for the repository (the durable *canonical* project context
remains the repository's own `docs/` tree — see
[`docs/repository-guide/canonical-docs`](docs/repository-guide/canonical-docs.md)).

## Layout

```
developer-docs/
├── docs/          Docusaurus content (the documentation you browse)
├── src/           landing page + site styling
├── static/        static assets
└── artefacts/     working artifacts of this site, kept in-repo by project
                   convention: research notes, generation scripts, and the
                   generated commit index sources
```

- `docs/` — the actual documentation, authored as Markdown/MDX.
- `artefacts/research-notes/` — survey notes produced while researching the
  current state of the repo (the source-of-truth snapshot used to write this
  site).
- `artefacts/scripts/` — small scripts that generate content (e.g. the commit
  index) from the git history.

The commit index (`docs/history/commit-index.md`) is a *generated* file: the
script in `artefacts/scripts/` regenerates it from `git log`, so it never drifts
from history — see "Regenerating the commit index" below.

## Getting started

```bash
cd developer-docs
npm install
```

### Local development

```bash
npm run start
```

Starts a local dev server (default `http://localhost:3000`) with live
reloading.

### Build and preview the static site

```bash
npm run build   # emits ./build — fails on broken links / build errors
npm run serve   # serves the built ./build locally
```

A clean production build is **required** before considering docs work done:
`onBrokenLinks` is set to `throw`, so broken internal links fail the build.

### Typecheck

```bash
npm run typecheck
```

## Regenerating the commit index

The commit index (`docs/history/commit-index.md`) is generated from git history,
not hand-written:

```bash
node artefacts/scripts/generate-commit-index.mjs
```

Run it from the **repo root** (one level above `developer-docs/`) so it can read the
monorepo's git history. Re-run it whenever meaningful commits land.

## Editing content

- Every page starts with front matter (`title`, `description`) and optionally a
  `sidebar_position`.
- Mermaid diagrams are supported inline via ` ```mermaid ` fences.
- GitHub links should always point at the real repository
  (`https://github.com/rushi-dynmsh/agentgate-dev`) — never invent org/repo
  names or commit SHAs.
- Keep content current with the source code: the live implementation is the
  authority. Flag anything you could not verify rather than guessing (use an
  explicit ⚠️ note).

## Deployment

No deployment target is fixed yet. `docusaurus.config.ts` carries
GitHub-Pages-shaped placeholder values for `url`/`baseUrl`; update them when the
site gets a host. See the Docusaurus [deployment docs](https://docusaurus.io/docs/deployment).