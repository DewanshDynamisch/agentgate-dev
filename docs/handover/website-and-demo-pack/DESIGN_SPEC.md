# AgentGate v1: Website, Brand and Demo Pack — Design

**Date:** 2026-10-09 · **Status:** awaiting owner approval · **Base:** `feature/v1-production-readiness` (v1.0.0-rc) · **Next step after approval:** implementation plan (writing-plans), executed by the implementation agent and verified by the planner.

## 1. Purpose and audience
Management will see a live demo of AgentGate within ~5 days; the product must look credible and be easy to explain. Beyond the demo, a developer outside the team must be able to put AgentGate in front of **their own MCP server through agentgateway** using only these docs.

Two audiences, two sites, kept separate:
- `developer-docs/` — **contributors** (project structure). Out of scope; untouched.
- `website/` (new) — **users and integrators**: landing page + documentation, one project, one build, one look.

## 2. Success criteria (each must be verifiable)
1. `cd website && npm ci && npm run build` succeeds with broken links treated as errors; `npm run serve` shows the site on localhost with **no network access** (offline demo: self-hosted fonts, local search, no CDN or analytics).
2. The quickstart's commands are executed by an automated check against the demo stack and pass. A doc whose commands fail fails CI.
3. Every code/config snippet that mirrors a repo file is **imported from that file**, never retyped (e.g. the agentgateway config from `deploy/demo/agentgateway.yaml`).
4. Landing page: Lighthouse (local run) accessibility ≥ 95 and performance ≥ 90 on desktop; animations respect `prefers-reduced-motion`.
5. The admin console and the website visibly share one brand (colours, type, logo); a script asserts the shared design tokens are identical.
6. The demo can be started with one command and reset with one command; a timed presenter script exists and every step in it has been executed once for real.
7. The README is accurate (version, status, licence, badges) and links resolve.

## 3. Scope
| ID | Deliverable |
|---|---|
| W1 | Brand kit |
| W2 | Website: landing page + integrator docs |
| W3 | README refresh |
| W4 | Demo-day pack (one-command demo mode, presenter script, automated screenshots and walkthrough video) |
| W5 | Admin console polish (cosmetic only) |
| W6 | Diagram set (shared by README, docs and landing) |

**Parked stretch (not planned now):** a "decision explainer" panel in the admin console; an in-browser Cedar policy playground. **Not in scope:** blog, i18n, versioned docs, analytics, comments, public hosting (blocked by the open licence decision), any change to product logic.

## 4. Constraints
- **No invented facts.** Docs may only state behaviour that was run or is in code/docs already verified. Anything unverified is omitted or listed as a limitation. Known limitations come from `docs/OPERATIONS/RUNBOOK.md` §12.
- **No new claims of openness.** The licence is undecided; the site and README say "licence: to be decided" and do not use the words "open source".
- **No external assets.** Logo and diagrams are hand-authored SVG; no stock images, no hotlinked fonts or scripts.
- The product (Go services, deploy topology) is not changed by this work. Exceptions are only the demo-mode script and the admin console's presentation layer.
- Standing project rules apply (see HANDOFF.md and the hardening plan): pathspec commits, no AI/agent attribution, PowerShell scripts executed in the Docker sandbox, never stage the quarantined `.ps1` files or the untracked stray files at the repo root (`h2.bundle`, `h3.bundle`, `update*.ps1`).

## 5. Brand kit (W1)
- **Name/tagline:** "AgentGate" — "A checkpoint in the only path between an AI agent and its tools." (the README's existing line; reused, not reinvented).
- **Palette (taken from the admin console's `frontend/app/src/styles/tokens.css`):** accent indigo `#4338ca` (hover `#3730a3`, soft `#e0e7ff`); semantic **allow** `#15803d`, **deny** `#b91c1c`, **candidate** `#a16207`; neutrals from the same file. A dark variant of every token is defined once and used by both the website and the console.
- **Type:** Geist (sans) and Geist Mono, self-hosted via the `@fontsource` packages (implementer verifies exact package names and licence).
- **Logo:** a simple geometric mark (a gate/checkpoint motif) plus a wordmark, hand-authored as SVG in light, dark and monochrome variants, plus favicon (SVG + 32px PNG) and a 1200×630 social-share image.
- **Location:** canonical assets in `brand/` (`tokens.css`, `logo/*.svg`, `og.png`, `favicon.*`). Each of `website/` and `frontend/app/` keeps its own copy of the shared tokens; `scripts/check-brand-tokens` fails if the shared subset diverges.

## 6. Website (W2)
**Stack (ruled):** Docusaurus 3, same major as `developer-docs` (3.10.x); Mermaid via `@docusaurus/theme-mermaid`; offline search via `@easyops-cn/docusaurus-search-local`; no Algolia; no other runtime dependencies without recorded justification. Static output in `website/build`.

**Landing page (`src/pages/index.tsx`), sections in order:**
1. Hero: tagline, one-sentence promise, two buttons (Quickstart, How it works), the animated request flow.
2. The problem: three short statements (agents act on real systems; nothing checks each call; no record of why).
3. How it works: agent → agentgateway → AgentGate → tool, with the four steps (identify, decide, credential, record) — copy adapted from `AgentGate_Plain_Language_Tour.md`.
4. **See it block:** an attack player that replays scenarios from the real security gate (see below).
5. Operator console: screenshots/video from W4.
6. Trust points: fails closed; audit-before-allow with a tamper-evident hash chain; agents never hold backend credentials; every claim links to the doc page that backs it.
7. Quickstart teaser (3 commands) and links; footer with status "v1.0.0-rc" and the limitations link.

**Interactive components (React, no new libraries):**
- `FlowAnimation` — SVG + CSS; a request travels the path and lands green (allowed) or red (denied); loops; static frame when reduced motion is requested.
- `AttackPlayer` — plays through `website/src/data/scenarios.json`, a hand-authored list drawn **only** from the 17 scenarios in `agentgate/qa/v1security` (each entry carries the test name it comes from, the plain-English attack, the observed result and the HTTP status). A repo test asserts every entry's test name exists in the gate suite.

**Docs information architecture (integrators):**
1. Introduction and concepts (identity, tool, risk, policy, decision, audit).
2. **Quickstart (15 min)** — based on `deploy/demo`: start the stack, get a token, call a tool through the gateway, see allow and deny, see the audit row. Commands marked as verified are run by the CI check.
3. **Integrate with agentgateway** — the gateway config (imported from `deploy/demo/agentgateway.yaml`): strict JWT auth, ext_authz gRPC, request-body inclusion, and the verified fact that agentgateway applies AgentGate's response headers (credential overwrite, inbound token removal).
4. Identity: OIDC/Keycloak setup, required claims (`sub`, `roles`, `obo`), role mapping, the trust boundary (identity only from the gateway-verified token).
5. Tools and risk classification: registering tools, the catalog API, risk levels, schema drift behaviour, unknown tools denied.
6. Policies: Cedar basics with the baseline's meaning, candidate → dry-run → activate → rollback, a recipe for allowing a destructive tool to one role.
7. Downstream credentials: what AgentGate mints (claims, scopes, lifetime, audience), JWKS endpoint, and **how to verify it in your own backend** (step list; references `deploy/demo/support-desk-mcp` as the worked example).
8. Audit: the record fields, hash chain, `verify-audit`, what is redacted.
9. Operations: production deployment, TLS/mTLS, metrics, restart/backup, upgrade notes (condensed from the runbook).
10. API reference — generated at build time from `agentgate/internal/apidocs/openapi.yaml` by a small script in `website/scripts/` (no OpenAPI plugin dependency).
11. Security model and known limitations (from the runbook §12; kept in sync by a script that fails if the limitation list in the runbook gains an entry the site lacks).
12. FAQ and troubleshooting (only symptoms that were actually observed).

**Verification of docs (the quality bar):** `website/scripts/verify-quickstart` extracts fenced blocks tagged `verify` from the quickstart page and executes them against the demo stack; CI job `docs-quickstart` runs it on Linux. The run happens in the Docker sandbox first.

## 7. README refresh (W3)
Rewrite top to bottom: one-line pitch, the flow diagram, a short "what you get" list, a 5-minute quickstart that links to the docs, an honest status line (v1.0.0-rc, what was verified and how), the known limitations link, accurate badges. Fix the stale parts found: it says "Active development (G5 Closed)"; the CI badge points at a placeholder repository slug (use the real remote from `git remote -v` or drop the badge); the Go badge says 1.26 but `agentgate/go.mod` says 1.25. Keep licence as "to be decided". Every relative link is checked by a script.

## 8. Demo-day pack (W4)
- **Demo mode script** (`deploy/prod/scripts/demo-mode.ps1`, plus a shell equivalent for Linux/CI): brings the production stack up (reusing `init-secrets`), starts the demo-agent traffic generator, waits for readiness, prints the URLs and the demo login, and supports `-Reset` (restore the backend's seed data and the baseline policy and tool classifications, restart the traffic generator). Idempotent.
- **Presenter script** (`docs/DEMO_SCRIPT.md`, local-only): about 12 minutes, timed; each step has the exact click or command and the expected result. Flow: landing page → console dashboard with live traffic → one allowed and one denied call → reclassify a tool live → policy dry-run/activate/rollback → audit page → `verify-audit`. It lists the fallback if a step fails. Every step is executed once before the document is declared done.
- **Automated captures:** a Playwright script (dev dependency in `website/`, run in the sandbox against the production stack) logs in through Keycloak and produces (a) PNG screenshots of the dashboard, audit, tools and policies pages, (b) one 60–90 s walkthrough video (WebM) of the main flow. Outputs go to `website/static/media/`. If login automation proves impossible, the implementer stops and reports rather than faking screenshots.

## 9. Admin console polish (W5, `frontend/app/`)
Presentation only; data logic untouched; no new dependencies. (1) Branded SSO/login screen with logo and tagline. (2) Dark theme using the shared dark tokens, a toggle persisted in `localStorage` (guarded with try/catch), default from `prefers-color-scheme`. (3) Consistent empty, loading and error states across pages. (4) Subtle page transitions and hover states, disabled under reduced motion. (5) Favicon and document titles per page. (6) Visible keyboard focus and sufficient contrast in both themes (checked with an automated contrast test on the token pairs). Existing tests stay green; new tests cover the theme toggle, token parity and the login screen.

## 10. Diagrams (W6)
One canonical set of hand-authored SVGs in `brand/diagrams/`: system flow, request journey (the four steps), credential flow, policy lifecycle. Reused by README (as SVG), docs (as images) and landing (as the animation's base). Mermaid is used only for diagrams inside docs pages where a sketch suffices.

## 11. Testing and evidence
Website: build with `onBrokenLinks: 'throw'`; the scenario-data test; the quickstart verification; a Lighthouse run whose scores are recorded. Brand: token-parity script and contrast test. README: link check. Demo: the real end-to-end run of `demo-mode` and the presenter script. Console: existing suites plus new tests; `npm run build`. The planner independently re-runs each of these before sign-off.

## 12. Decisions the owner still owns
Product name/logo direction approval (the mark is a placeholder-quality original until a designer replaces it); hosting and domain; the licence (blocks any public publishing); whether the stretch items come back later. None blocks implementation.

## 13. Order
W1 → W6 (diagrams) → W2 (site; docs content parallelisable with landing) → W5 → W4 → W3 → final verification. W2 and W5 depend on W1; W4 depends on W2/W5 for the pages it captures.
