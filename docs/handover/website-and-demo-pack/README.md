# Handover: Website, Brand & Demo Pack

Branch: `feature/website-and-demo-pack` (based on `feature/v1-production-readiness`, which is the hardened v1.0.0-rc). Nothing in this folder is a new idea — it's the plan and spec that were already approved; this file just tells you where to start and what's already done.

## Read in this order
1. This file.
2. [`DESIGN_SPEC.md`](DESIGN_SPEC.md) — what we're building and why. Skim it once.
3. [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) — the actual work, broken into Tasks T1–T20. This is what you follow. It's written in a task-by-task style (files to touch, exact behaviour, test cases, commands) — treat each task like a mini ticket.

**One note on the plan's wording:** it was written for an AI agent working on the original developer's machine, which had a corporate antivirus (Bitdefender) falsely flagging PowerShell scripts, so it routes `.ps1` work through a "Docker sandbox" workaround with git bundles. **Ignore all of that — it doesn't apply to your machine.** Just run the commands directly (PowerShell 7 / `pwsh`, Docker Desktop) and commit normally. Wherever the plan says "edit in the sandbox," just edit the file. Also ignore any line about "AI/agent attribution" in commit messages — that was for the AI agent; use your own normal commit style, just keep messages clear and conventional (`feat(...)`, `fix(...)`, `docs(...)`, `test(...)`).

## What's already done (don't redo it)
- **T1 — Brand kit:** `brand/tokens.css` (colors, light + dark), `brand/logo/*.svg`, `brand/favicon.svg`, and `tools/check-brand-tokens.mjs` (a script that fails if the website/console/brand color tokens drift apart). Committed.
- **T2 — Diagram set:** `brand/diagrams/*.svg` (system flow, request journey, credential flow, policy lifecycle) + `tools/check-svg.mjs` (fails on unsafe/external-referencing SVGs). Committed.
- **T3 — Website scaffold: in progress, not finished.** A Docusaurus 3 site exists at `website/` with the brand colors wired in and an offline-resource checker (`website/scripts/check-offline.mjs`), but it's incomplete — **resume it, don't restart it.** Read Task 3 in the plan for what's still missing (the full landing-page shell, nav, footer, etc.).

Everything from T4 onward is **not started.**

## How to verify as you go
From the repo root:
```
node --test tools/test/*.test.mjs
node tools/check-brand-tokens.mjs --files brand/tokens.css frontend/app/src/styles/tokens.css website/src/css/tokens.css
node tools/check-svg.mjs brand
```
From `website/`:
```
npm ci
npm run build
npm run typecheck
npm test
npm run check:offline
```
(`npm test` already runs `node --test scripts/test/*.test.mjs` — don't use the bare-directory form `node --test scripts/test`, it silently reports 0 tests on current Node versions.)

From `frontend/app/` (only touched in Tasks 13–14):
```
npm run build
npm test
```

## Rules that still apply
- **Tests first.** Each task in the plan tells you the exact test cases — write them, watch them fail, then implement.
- **No invented facts in docs.** Every factual claim on the website/docs must trace back to something you actually ran or to a real file in the repo (the plan calls this out per-task).
- **Don't touch other people's work in progress.** `agentgate/`, `deploy/`, `.github/workflows/ci.yml`, and `docs/OPERATIONS/` belong to a separate hardening effort that's still finishing up. The plan marks exactly which late tasks (12, 15, 17) touch those — leave them until you hear those files are clear, or ask first.
- **Licence is undecided.** Never write "open source" anywhere on the site or in docs; say "licence: to be decided."
- **Keep a running note of progress** somewhere convenient for you (a scratch file, a project board, whatever your team uses) — the plan's own task checkboxes are the source of truth for *what* to do, but it helps to track which ones are actually done and verified.

## Questions / blockers
If a task's assumption turns out wrong, or you hit a real design decision the plan doesn't answer, stop and ask rather than guessing — same as any ambiguous ticket.
