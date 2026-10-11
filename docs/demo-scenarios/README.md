# AgentGate Demo Scenarios & Execution Guide

Welcome to the end-to-end execution guide for AgentGate. This repository contains everything needed to demonstrate the project from scratch, even if you have never inspected the underlying code.

---

## What is AgentGate? (The 30-Second Elevator Pitch)
> "When AI agents invoke tools, traditional firewalls and IAM cannot see what the agent is doing. AgentGate sits as a zero-trust checkpoint directly between the AI agent and the tools. It inspects every request in-flight, enforces Cedar security policies, mints short-lived credentials so the agent never holds raw backend keys, and records every decision into a tamper-evident audit hash chain."

---

## System Components Quick Map

When the demo stack runs, here is what is running on your machine:

| Component | Port / URL | Description |
| :--- | :--- | :--- |
| **Admin Console** | `https://localhost:8444` | The web UI for security operators (Audit logs, Tool catalog, Policies). |
| **Documentation & Landing** | `http://localhost:3000` | The public landing page with interactive attack scenario player. |
| **Agent API Gateway** | `https://localhost:3443` | The entrypoint for agents making tool calls (Envoy / agentgateway). |
| **AgentGate Control Plane** | `https://localhost:9443` | The core Go engine evaluating Cedar policies and minting tokens. |
| **Keycloak (IdP)** | `https://localhost:8443` | Enterprise Identity Provider issuing agent and operator credentials. |
| **PostgreSQL** | `localhost:5432` (internal) | Storage for policies, tool schemas, and cryptographic audit logs. |
| **Support Desk MCP** | `localhost:9200` (internal) | Mock backend tools server (`list_tickets`, `delete_ticket`, etc.). |
| **Demo Agent Worker** | (Background container) | Continuously sends simulated agent traffic so the dashboard is live. |

---

## Pre-Flight Checklist (Do this first)

1. **Start Docker Desktop:** Ensure Docker Desktop is running on Windows (`docker ps` should work in PowerShell).
2. **Start the Demo Stack:**
   Open a PowerShell terminal in the repo root:
   ```powershell
   .\deploy\prod\scripts\demo-mode.ps1 -Reset
   ```
   *(Wait ~60 seconds until the terminal prints `AgentGate Demo Environment is Running`).*
3. **Start the Documentation Portal:**
   Open a second PowerShell terminal:
   ```powershell
   cd website
   npm run serve -- --port 3000
   ```
4. **Open Browser Tabs:**
   - **Tab 1:** `http://localhost:3000` (Landing page & Attack player)
   - **Tab 2:** `https://localhost:8444` (Admin Console. Accept self-signed cert warning, click "Sign in with SSO", log in as `admin` with password from `deploy/prod/secrets/admin_initial_password`).

---

## The 7 Demo Scenarios

Execute these scenarios in sequence or jump to any specific one:

1. **[Scenario 1: Allowed Tool Call (The Happy Path)](./01-allowed-tool-call.md)**
   *Demonstrates normal authorized agent execution (`list_tickets`) and minting of ephemeral credentials.*

2. **[Scenario 2: Privilege Escalation Denied (Policy Block)](./02-privilege-escalation-denied.md)**
   *Demonstrates an agent attempting an unauthorized destructive action (`delete_ticket`) being blocked at the gateway.*

3. **[Scenario 3: Live Tool Risk Reclassification (Dynamic Governance)](./03-live-tool-reclassification.md)**
   *Demonstrates changing a tool's risk level live in the UI and watching subsequent calls instantly get blocked without restarting any service.*

4. **[Scenario 4: Unknown / Unregistered Tool Injection Blocked](./04-unknown-tool-injection.md)**
   *Demonstrates how strict schema/whitelist enforcement blocks hallucinated or injected tool calls.*

5. **[Scenario 5: Policy Lifecycle (Dry-Run & Instant Rollback)](./05-policy-dryrun-and-rollback.md)**
   *Demonstrates creating a candidate Cedar policy, testing impact against historical traffic with dry-run, and rolling back.*

6. **[Scenario 6: Cryptographic Audit Hash Verification](./06-cryptographic-audit-verify.md)**
   *Demonstrates running the CLI verification tool to mathematically prove audit log integrity and tamper resistance.*

7. **[Scenario 7: Public Portal & Offline Attack Player](./07-offline-and-attack-player.md)**
   *Demonstrates the public portal, offline local search, and interactive security scenario stepper.*
