# AgentGate Frontend Implementation Guide

**Status:** Current implementation guide
**Frontend application:** `frontend/app/`
**Shared contract layer:** `frontend/src/`
**Last reviewed:** 2026-09-18

## 1. What the Frontend Does

The frontend is the operator console for AgentGate. A human administrator uses it to manage authorization policies.

The frontend does not sit in the live MCP request path. Runtime agent requests use this path:

```text
AI agent -> agentgateway -> AgentGate ext_authz -> MCP server
```

The operator governance path uses this path:

```text
Human operator -> React UI -> AgentGate REST API -> policy store
```

## 2. Folder Structure

```text
frontend/
├── app/                         Actual React + Vite application
│   ├── src/
│   │   ├── components/          Shared UI components and layouts
│   │   ├── pages/               Dashboard, policies, tools, audit, etc.
│   │   ├── state/               React providers and app state
│   │   ├── styles/              Design tokens, layout, component CSS
│   │   ├── test/                Test setup, fixtures, and workflow tests
│   │   ├── App.tsx              Application routes
│   │   └── main.tsx             React entry point
│   ├── package.json              UI scripts and dependencies
│   ├── vite.config.ts            Vite development/build configuration
│   ├── vitest.config.ts          Test configuration
│   └── PROPOSED_G8_API_CONTRACTS.md  Draft contracts for future read APIs
├── src/                         Framework-independent contract layer
│   ├── api/                     GovernanceClient, mock and HTTP clients
│   ├── models/                  Governance, decision, tool, and identity types
│   ├── parsing/                 Backend response parsing
│   ├── state/                   Policy lifecycle store
│   ├── view/                    Display-safe view models
│   └── fixtures/                Decision and UI fixtures
├── test/                        Contract-layer Vitest tests
├── dist/                        Generated contract-layer build output
└── package.json                 Contract-layer scripts and dependencies
```

`frontend/app` imports the shared layer through the TypeScript/Vite alias:

```text
@contract/* -> ../src/*
```

There is no separate `admin-ui/` directory in this checkout. `frontend/app` is the actual React application.

## 3. Backend Connection

Outside tests and an explicit dev mock opt-in, the React app uses `HttpGovernanceClient`, authenticated with the signed-in user's OIDC access token (authorization code + PKCE via `oidc-client-ts`). Runtime config (`apiUrl`, `oidcAuthority`, `oidcClientId`, `oidcScope`) comes from `/config.js` (`window.__AGENTGATE_CONFIG__`), rendered by the container entrypoint; empty OIDC values show an "SSO not configured" state.

```text
React page
  -> GovernanceProvider
  -> PolicyLifecycleStore
  -> HttpGovernanceClient
  -> Vite /api proxy
  -> AgentGate HTTP API on :8090
```

The Vite development proxy is configured in `frontend/app/vite.config.ts`:

```text
/api -> http://127.0.0.1:8090
```

The client sends the administrator token as:

```http
Authorization: Bearer <admin-token>
```

In the dev server with `VITE_USE_MOCK=true` (and in tests), the app uses `MockGovernanceClient` and seeds deterministic demo policies. This is intentional for UI development, but it must not be mistaken for live backend data.

## 4. Live Connected Features

These frontend operations call the real AgentGate governance REST API when live mode is enabled:

- List policies
- Validate Cedar policy
- Create candidate policy
- Dry-run active versus candidate policy
- Activate a policy
- Roll back to a historical policy
- Display active, candidate, and historical policy states
- Display active policy details and Cedar source
- Filter policies by All, Active, Candidates, and History

The connected API routes are:

```http
GET  /api/v1/workspaces/{workspace_id}/policies
POST /api/v1/workspaces/{workspace_id}/policies/validate
POST /api/v1/workspaces/{workspace_id}/policies
POST /api/v1/workspaces/{workspace_id}/policies/{version}/dryrun
POST /api/v1/workspaces/{workspace_id}/policies/{version}/activate
POST /api/v1/workspaces/{workspace_id}/policies/rollback
```

## 5. Policy Lifecycle

A policy moves through these states:

```text
Create -> Candidate -> Validate/Dry-run -> Active -> Historical
                                      ^          |
                                      |          |
                                      +-- Rollback
```

### Candidate

A proposed policy. It is stored but does not affect live authorization.

### Active

The policy currently used by AgentGate to authorize tool calls. Normally there is one active policy per workspace.

### Historical

A previously active policy retained for audit, comparison, and rollback.

The All count includes all three states:

```text
All = Active + Candidates + Historical
```

## 6. Policy UI Features

### Dashboard

The dashboard displays live policy lifecycle data when connected to the backend:

- Total policy count
- Active version
- Candidate count
- Historical count
- Recent policies
- Active policy card

Clicking the active policy card opens a detail dialog with:

- Full version
- Workspace
- Status
- Activation time
- Description
- Full Cedar source

### Policies page

The policies page provides:

- All/Active/Candidates/History filters
- Policy version display with copy action
- Policy status
- Description
- Updated time
- Dry-run navigation for candidates
- Rollback navigation for historical policies

Clicking a policy row opens its detail dialog. Dry-run and rollback buttons continue to perform their own actions.

### Create policy

The create screen provides:

- Policy name field
- Description field
- Cedar source editor
- Backend-aligned starter policy
- Cedar validation
- Candidate creation

The starter policy uses AgentGate's actual authorization model:

```cedar
AgentGate::Role::"reader"
AgentGate::Role::"admin"
AgentGate::Role::"payer"
AgentGate::Action::"InvokeTool"
resource.risk == "read" | "write" | "destructive"
```

### Dry run

The dry-run screen compares representative requests against the active and candidate policies.

Current examples use:

```text
reader + read_status  + read         -> ALLOW
reader + write_status + write        -> DENY
admin  + write_status + write        -> ALLOW
admin  + read_status  + read         -> ALLOW
reader + destructive tool             -> DENY
```

### Activation and rollback

Activation requires confirmation and does not navigate or show success until the governance client confirms the backend response.

Rollback calls the backend rollback endpoint and refreshes the policy state after confirmation.

## 7. Current Mock or Static Pages

These pages are not connected to live backend read APIs yet:

### Tools & resources

The page and detail popup currently use `FIXTURE_TOOLS` in `frontend/app/src/pages/Tools/ToolsPage.tsx`.

The backend tool registry exists, but a frontend read endpoint is not implemented yet.

### Audit log

The page currently uses fixture rows in `frontend/app/src/pages/Audit/AuditPage.tsx`.

The backend durable audit store exists, but a frontend audit query endpoint is not implemented yet.

### Identities

The page currently uses placeholder identity data. A live identity-mapping read API is not connected.

### Settings

The page currently displays static configuration information. It does not load or update backend settings.

## 8. Pending Frontend Work

### Backend read APIs

Wait for the backend contracts and endpoints before wiring these pages:

```http
GET /api/v1/workspaces/{workspace_id}/audit-events
GET /api/v1/workspaces/{workspace_id}/tools
```

The proposed response shapes are documented in:

```text
frontend/app/PROPOSED_G8_API_CONTRACTS.md
```

Do not connect the UI to guessed response shapes.

### Authentication

The current login uses one static development bearer token. Production authentication still needs:

- OIDC login
- Multiple users
- Admin permissions
- Token refresh
- Real session management

### Activation reason

The activation page has a reason field for operator context, but the current backend activation API does not accept or persist that reason. It is currently UI-only.

### Decision Tester

The ticket references a Decision Tester page, but no such page exists in this checkout. Existing decision fixtures are tested directly in:

```text
frontend/app/src/test/decision-fixtures.test.ts
```

A Decision Tester screen should be a separate scoped feature rather than being silently invented as part of the current work.

## 9. Running the Frontend

### Live backend mode

Terminal 1:

```powershell
cd D:\agentgate-dev\agentgate
$env:AGENTGATE_ADMIN_TOKEN = "agentgate-admin-secret-dev"
go run ./cmd/agentgate
```

Terminal 2:

```powershell
cd D:\agentgate-dev\frontend\app
# Edit frontend/app/public/config.js: set apiUrl, oidcAuthority, oidcClientId
npm run dev
```

Open:

```text
http://localhost:5173
```

The current simple backend run uses in-memory policy and audit storage. Data resets when AgentGate stops.

### Mock/demo mode

From `frontend/app`:

```powershell
$env:VITE_USE_MOCK = "true"
npm run dev
```

Mock mode is never used in production builds; there is no static admin token in the UI.

## 10. Verification Commands

For the React application:

```powershell
cd D:\agentgate-dev\frontend\app
npm test
npm run build
npm run lint
```

Current committed app tests cover:

- Policy tab filtering
- Activation waiting for backend confirmation
- Mock policy lifecycle: list, validate, create, dry-run, activate, rollback
- Decision fixture rendering for ALLOW, DENY, transport errors, and stale state

For the shared contract layer:

```powershell
cd D:\agentgate-dev\frontend
npm run typecheck
npm test
npm run build
```

## 11. Important Boundaries

- The website manages governance; it does not execute MCP tools.
- `agentgateway` handles MCP transport and routing.
- AgentGate handles identity, policy, authorization, and audit.
- MCP servers execute the actual business operations.
- Tool and audit fixture data must not be presented as live backend data.
- The frontend must never show an activation as successful before the backend confirms it.
- The frontend must not bypass AgentGate or connect agents directly to an MCP backend.
