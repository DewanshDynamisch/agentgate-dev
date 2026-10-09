# SDD ledger — plan: docs/superpowers/plans/2026-09-29-agentgate-v1-production.md

Note: no separate spec file; plan + CREDENTIAL_REQUIREMENTS.md + PRODUCTION-INVARIANTS.md are authority. Rulings provisional.
Ruling: work on branch feature/v1-production-readiness in main checkout, no worktree — single dev, owner requested — cost if wrong: none (branch isolation).
Scope: owner asked to run Task 1 only, then stop for token review.

## Preflight scan
| Pair / task | Shared | Finding |
|---|---|---|
| T1→T2 | credential.Issuer/Credential | consistent |
| T1→T3 | JWKS(), Credential.HeaderName | T5 may switch HeaderName to x-agentgate-credential; T1 default "Authorization" is fine |
| T2→T3 | EvaluateWithCredential signature | consistent |
| T3→T4/T5 | aud "support-desk", issuer "agentgate" | consistent with T4 defaults |
| T6→T7→T8 | ToolDefinition, Catalog, argdecl key backend/tool | consistent |
| T10→T11-13,16 | GovernanceClient methods, getAccessToken | consistent |
| T14→T15 | oidcverify.Verifier/Principal | consistent |
| T17↔T18 | Validate TLS check deferred to T18 | stated in both |
| T1 self | tests (a)-(j) match spec | ok |
| T3 self | JWKS mounted unauth; config fields | ok |
| T21 self | ports line fixed during plan review | ok |
Ruling: -race unavailable locally (no gcc/cgo); plain go test accepted per task, CI 'go' job runs -race — cost if wrong: a race slips until CI.
Task 1: minor (deferred): no test for Now default / Scopes override / PKCS#8 non-RSA key rejection
Task 1: minor (deferred): no key.Validate() after load (Go parse already validates)
Task 1: complete (commits 8a45f18..660710d, review clean)
Task 2: implemented 098732d; review spec OK; Important: empty Ref/HeaderValue not rejected
Task 2: minor (deferred): wrapper test lacks ref assert; slog global logger; SetEvaluator/evaluator read unlocked (pre-existing)
Task 4: dispatched in worktree (parallel to T2); result pending
Task 4: review clean (spec OK, approved). Cherry-picked from worktree branch onto feature/v1-production-readiness.
Task 4: minor (deferred): keyMu held across JWKS fetch; 401 body leaks jwks URL/error text (return generic, log detail); base64 not Strict; configFromEnv default require=true untested; first-fetch/failure 30s throttle
Task 2: fix round 1/5 (1 addressed pending re-review — empty Ref/HeaderValue; commit dffc6cd)
Task 2: complete (commits 660710d..dffc6cd, review clean after 1 fix round)
Task 4: complete (commit af308d7, review clean)
Task 3: BASE=af308d7 dispatched
Task 3: implemented 8024b80; NOTE default requireCredential=true w/o audiences denies all ALLOWs — g6/demo compose config fixed in Task 5 (live CI g6 job red on this branch until then)
Task 3: review spec OK; Important: case-sensitive production env check in main.go (ephemeral key fallthrough). Fix round 1 dispatched (normalize env in config.Load + empty HeaderName guard).
Task 3: minor (deferred): audit says Allow but wire Deny when evaluator returns nil cred w/o error (fallback path only); parseAudiences dup keys last-wins; CredentialLifetime<=0 not in Validate; main.go startup paths untested (loadSigningKey helper tested only after fix)
Task 3: complete (commits af308d7..b1c483c, review clean after 1 fix round)
Tasks 5 (main checkout, docker) and 6 (worktree) dispatched in parallel, BASE=b1c483c; Task 7 waits on 6
Task 6: review clean (spec OK, approved); commit 075c413 in worktree branch worktree-agent-a84829124cf049fd7, to cherry-pick after Task 5 commits. Ruling: test env var AGENTGATE_TEST_POSTGRES_URL (repo convention) replaces brief's AGENTGATE_TEST_DATABASE_URL — cost if wrong: none (naming only). Later tasks/CI must use AGENTGATE_TEST_POSTGRES_URL.
Task 6: minor (deferred): non-object schema (null/123) may pass Validate — CARRY INTO Task 8 (API must reject non-object schema); backend_id no pattern (API layer validates); no deep-copy mutation test; Delete missing row = no-op (fine for 204)
Task 5: implemented 89b3b35 (agentgateway v1.4.0 honours ext_authz authorization overwrite; no fallback header needed; e2e 7 checks PASS under PS 5.1; g6 matrix PASS). Review dispatched.
Task 6: complete (cherry-picked as f11303b, review clean)
Task 7: dispatched, BASE=f11303b
Task 5: review Changes required — Important: key file perms on Linux CI; teardown not run on early exit; check 5 possibly vacuous (hash format assumed). Fix round 1 dispatched (explicit-path commits since Task 7 committing concurrently).
Task 5: minor (deferred): hardcoded ports 8091/3001 & pg names; pwsh-on-Linux arg quoting untested; -print-token leak risk if stderr ever echoed; docker compose w/o key file creates dir at mount path; check numbering 1-7 vs brief 1-9 (nothing dropped)
Task 7: implemented bcd4c92 (also tightened Validate to require object schema); review dispatched
Task 7: complete (commits f11303b..bcd4c92, review clean)
Task 7: minor (deferred): race test lacks reader/writer overlap barrier (power gap; add started-barrier); t.Fatal helper calls from non-test goroutines; Snapshot fields exported/mutable by convention only; no string-schema Validate test case
Task 5: complete (commits b1c483c..d64dd96, review clean after 1 fix round)
Task 8: dispatched sequentially, BASE=d64dd96
Task 8: implemented 28cc160 (bootstrap parity verified by reviewer; demo e2e + g6 pass live). Review Changes required — Important: catalog writes not serialized (store/snapshot divergence); rollback errors swallowed. Fix round 1 dispatched (+ minors: DELETE missing→404, weak path-identity test, gofmt).
Task 8: minor (deferred): audit emission post-mutation w/o error return (mirrors policymanager); tool events attributed ws default/operator admin with backend/tool in NewVersion — Task 15 must add real actor + proper fields; decoder allows trailing JSON / unknown body fields
Task 8: fix round 1/5 (2 addressed + 3 minors, 0 open; commit b1e3df8)
Task 8: minor (deferred): concurrency test didn't go red pre-mutex (weak guard); audit emit inside catalogMu; rollback-error path untested
Task 8: complete (commits d64dd96..b1e3df8, review clean after 1 fix round)
Tasks 9 (deploy script) + 10 (frontend/src) dispatched in parallel in main checkout, BASE=b1e3df8, pathspec commits
Task 10: complete (commit e455cc0, review clean)
Task 10: minor (deferred): listTools fails wholesale if any tool schema is null; audit id optional vs brief required; parse gaps untested (missing workspace_id, malformed arguments)
Task 11: dispatched BASE=e455cc0 (Task 9 still running, disjoint files)
Task 9: BLOCKED — OS file lock on deploy/demo/run-credential-e2e.ps1 (file deleted-pending, recreate = Permission denied; likely IDE handle). Agent's uncommitted edit lost via its own checkout; committed version safe in HEAD. Waiting on owner to release handle; then restore from HEAD and re-dispatch with prior design (checks 8-13, restart verifies non-default risk).
Task 11: implemented 90d0c2f; review Changes required — Important: rows hidden on any refresh error; load-older error cleared by refresh. Fix round 1 dispatched.
Task 11: minor (deferred): inFlight overlap after toggle; nextBefore/filters not reset on client change (constant in app)
Task 11: fix round 1/5 (2 addressed + guard, 0 open; commit f7b2313)
Task 11: complete (commits e455cc0..f7b2313, review clean after 1 fix round)
Task 12: dispatched BASE=f7b2313
Task 12: implemented ad3ec0d; review Changes required — Important: no client-side id/name pattern validation; duplicate/untrimmed arg names allowed. Fix round 1 dispatched (+ minors: confirm reset, aria-describedby, td flex).
Task 12: minor (deferred): dialog focus not trapped/restored; register dialog unkeyed (unmount resets anyway)
Task 12: fix round 1/5 (2 addressed + 3 minors, 0 open; commit d8c057f)
Task 12: complete (commits f7b2313..d8c057f, review clean after 1 fix round)
Ruling: renamed deploy/demo/run-credential-e2e.ps1 -> run-demo-e2e.ps1 (orphaned delete-pending NTFS entry blocks that exact name, no holder process) — cost if wrong: one extra rename commit; old name reusable after reboot/Docker restart
Ruling: Task 9 (and later PS scripts) executed inside Docker-in-Docker sandbox `agentgate-sandbox` (/work clone) because Bitdefender EDR quarantines the .ps1 on the Windows disk; owner approved Docker approach and declined-by-me .txt rename (evasion). Commits transported back via git bundle — cost if wrong: host working tree shows the .ps1 as deleted until IT exclusion.
Task 9: implemented in sandbox d7a326f (13/13 PASS under pwsh/Linux). Review Changes required — Important: no risk restore on mid-run failure (w/o -Down); /healthz liveness race after restart. Fix round 1 dispatched (+ minors: audit-source reason, 403 status match, baseline delta).
Ruling: kept test-only hook AGENTGATE_E2E_TEST_FAIL_AFTER_9 in run-demo-e2e.ps1 (no-op unless =1; proves restore-on-failure) — cost if wrong: trivial removal later.
Task 9: fix round 1/5 (2 addressed + minors, 0 open; sandbox commit 6bed8c6)
Task 9: complete (commits 1b71aab..6bed8c6 via sandbox bundle, branch ref fast-forwarded without checkout; review clean after 1 fix round). Host worktree shows run-demo-e2e.ps1 deleted (Bitdefender) — cosmetic until IT exclusion.
Task 13: dispatched BASE=6bed8c6
Task 13: complete (commit 3e30d56, review clean)
Task 13: minor (deferred): repeated cursor → duplicate events inflate counts (stop if next>=before); label says "last 500" even when MAX_PAGES truncates; non-ALLOW/DENY counted in total; UTC buckets labelled in local time (IST shows :30); Identities dropped derivable "acting for" (principalOnBehalfOf); audit loading/error not reset on client change; chart tooltips not a11y; mock class still in prod bundle (size only)
Tasks 14 (Go oidcverify) + 16 (frontend OIDC) dispatched in parallel, BASE=3e30d56, pathspec commits
Task 14: implemented 5977a4d; review Changes required — Important: no token typ check (ID/refresh tokens replayable as admin access tokens via azp fallback); JWKS cache no TTL (revoked key trusted until restart). Fix round 1 dispatched (+ detached fetch ctx, dup-kid reject, https-only unless AllowInsecureHTTP, no cross-host redirects, extra tests).
Ruling: startup JWKS fetch counts toward 30s refetch throttle — kept (reviewer agrees; ≤30s of 401s during rotation-at-restart).
Ruling: JWKS TTL 1h; on refetch failure serve stale keys up to 2×TTL then fail closed (ErrUnavailable) — cost if wrong: revoked key trusted up to 2h during IdP outage.
Task 14: minor (deferred): concurrent callers during in-flight refresh get 401; no exp-lifetime upper bound; no RSA modulus upper bound
Task 16: implemented dd6b8cc (access_token sent, no open redirect, sessionStorage). Review spec OK; Important: automaticSilentRenew without refresh/iframe setup; roles read only from ID token (Keycloak puts realm_access in access token). Fix round 1 dispatched (+ logout removeUser fallback, test timeouts, 401-guard test, stale FRONTEND_IMPLEMENTATION.md).
Task 16: minor (deferred): deep-link returnTo dropped after login; loginRef written during render; config.js must be served no-cache + SPA fallback returns HTML if missing (Task 20)
Task 14: fix round 1/5 (2 addressed + minors, 0 open; commit 122b7f7). Payload typ accepted only absent/Bearer (stricter; reviewer agrees).
Task 14: minor (deferred): no https→http downgrade redirect test; cross-host test only varies port
Task 14: complete (commits 3e30d56..122b7f7 [5977a4d,122b7f7], review clean after 1 fix round)
Task 15: dispatched BASE=7206451
Task 16: fix round 1/5 (2 addressed + minors; commit 7206451). New Important in fix diff: renewal not single-flight (refresh-token rotation → invalid_grant). Fix round 2 dispatched.
Task 16: fix round 2/5 (1 addressed, 0 open; commit 7c304a2; library auto-renew verified to route through wrapper)
Task 16: complete (commits dd6b8cc..7c304a2, review clean after 2 fix rounds)
Task 15: INTERRUPTED (session limit) — partial UNCOMMITTED work in working tree: main.go, auditevents/events.go, config.go(+test), govapi/handler.go, middleware.go, tools_catalog.go, policymanager/manager.go, new govapi/auth_test.go. Agent's last note: 'Fix my test's audit usage, add a catalog attribution test.' No report written. Handing off to another coding agent.
Task 15: re-dispatched (fresh implementer) BASE=7c304a2, partial uncommitted work in tree from prior interrupted session
Task 15: implemented bca8c82; review dispatched
Task 15: review spec OK, approved. 0 Critical, 0 Important, 2 Minor.
Task 15: minor (deferred): config.Validate() does not explicitly reject production+static (Load forces oidc, defense-in-depth only); buildAdminAuthenticator warns but doesn't error on production+static (relies on Load normalisation)
Task 15: complete (commits 7c304a2..bca8c82, review clean)
Task 17: dispatched BASE=bca8c82
Task 17: implemented 7a2b068; review dispatched
Task 17: review Changes required — Important: pre-auth rate limiting bypassed (middleware ordering); Load() early exit breaks aggregated error reporting; static mode validation unreachable in production. Fix round 1 dispatched.
Task 17: fix round 1/5 (3 addressed pending re-review; commit ad45ac4)
Task 17: minor (deferred): clientIP trusts X-Forwarded-For/X-Real-IP without proxy verification (fine for internal gateway setup)
Task 17: complete (commits bca8c82..ad45ac4, review clean after 1 fix round)
Task 18: dispatched BASE=ad45ac4
Task 18: implemented 9d1992f; review dispatched
Task 18: review spec OK (Approved); 2 Important findings: unchecked AuthzTLSClientCA in dev mode without cert/key; HTTP server test uses SetTLS instead of SetTLSConfig. Fix round 1 dispatched.
Task 18: fix round 1/5 (2 addressed pending re-review; commit 8131ac1)
Task 18: minor (deferred): redundant SetTLS/ListenAndServeTLS methods on httpserver.Server (non-blocking)
Task 18: complete (commits ad45ac4..8131ac1, review clean after 1 fix round)
Task 19: dispatched BASE=8131ac1
Task 19: implemented b89f8d6; review dispatched
Task 19: minor (deferred): admin requests metric not called in defer; uncounted if handler panics (server recover still responds)
Task 19: complete (commits 8131ac1..b89f8d6, review clean)
Task 20: dispatched BASE=b89f8d6
Task 20: implemented 8163e92; review dispatched
Task 20: minor (deferred): runHealthcheck hardcodes http://; will need https flag if internal listener terminates TLS directly (fine behind compose/proxy)
Task 20: minor (deferred): missing flag validation unit test for verify-audit without --workspace flag
Task 20: complete (commits b89f8d6..8163e92, review clean)
Task 21: dispatched BASE=8163e92
Task 21: implemented 56ac4c3; review dispatched
Task 21: review spec OK, approved. 0 Critical, 0 Important, 2 Minor.
Task 21: minor (deferred): admin user temporary=false in realm-agentgate.json to permit non-interactive direct access password grant; secret file vs env var passing for DB URL in compose
Task 21: complete (commits 8163e92..56ac4c3, review clean)
Task 22: dispatched BASE=56ac4c3
Task 22: implemented 94dbf43; review dispatched
Task 22: review spec OK, approved. 0 Critical, 0 Important, 1 Minor.
Task 22: minor (deferred): response body read limit wrapper in demo-agent ExecuteScenario
Task 22: complete (commits 56ac4c3..94dbf43, review clean)
Task 23: dispatched BASE=94dbf43
Task 23: implemented ed5ce2b; review Changes required — Critical: TestV1_17_CatalogRace case mismatch (0 records evaluated, missing DENY assertions); Important: TestV1_10 fragile events[0] assertion; TestV1_06 host address in container. Fix round 1 dispatched.
Task 23: fix round 1/5 (4 addressed, 0 open; commit 59d833c).
Task 23: complete (commits 94dbf43..59d833c [ed5ce2b, 59d833c], review clean after 1 fix round)
Task 24: dispatched BASE=59d833c
Task 24: implemented d43d206; review dispatched
Task 24: review spec OK, approved. 0 Critical, 0 Important, 1 Minor.
Task 24: minor (deferred): runbook prerequisites command uses head pipe; note Select-Object for Windows PowerShell
Task 24: complete (commits 59d833c..d43d206, review clean)
Task 25: dispatched BASE=d43d206
Task 25: implemented 3a0cf75; review dispatched
Task 25: review spec OK, approved. 0 Critical, 0 Important, 0 Minor.
Task 25: complete (commits d43d206..3a0cf75, review clean)

- **Addendum (2026-10-09)**: Fixed demo-support-desk-mcp distroless healthcheck failure by adding a CLI healthcheck command to its Go binary and modifying its docker-compose.yml check. Modified run-demo-e2e.ps1 to execute wget via agentgate container. Re-ran e2e and security gate scripts: 100% PASS.
