# Scenario 1: Allowed Tool Call (The Happy Path)

## 1. What This Proves (What to Say)
> "When an authorized AI agent needs to perform an operation, AgentGate does not get in the way. It verifies the agent's identity, evaluates the Cedar policy, confirms the tool is within the agent's permitted risk level, and mints a short-lived 60-second credential before passing the request to the tool server."

---

## 2. Pre-Conditions
- The stack is running (`.\deploy\prod\scripts\demo-mode.ps1 -Reset`).
- Admin Console is open at `https://localhost:8444/audit`.

---

## 3. Step-by-Step Execution

### Option A: Via Terminal (Quick & Direct)
Open PowerShell in the repo root and run this command block:

```powershell
# 1. Obtain an identity token from Keycloak for the 'agent-reader' identity
$agentSecret = (Get-Content deploy/prod/secrets/agent_reader_secret).Trim()
$tokenResp = curl.exe -k -s -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token `
    -H "Content-Type: application/x-www-form-urlencoded" `
    -d "grant_type=client_credentials&client_id=agent-reader&client_secret=$agentSecret"
$agentToken = ($tokenResp | ConvertFrom-Json).access_token

# 2. Invoke the 'list_tickets' tool via the AgentGateway
curl.exe -k -s -i -X POST https://localhost:3443/ `
    -H "Authorization: Bearer $agentToken" `
    -H "Content-Type: application/json" `
    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"list_tickets\",\"arguments\":{}}}'
```

### Option B: Observe the Automated Background Traffic
If you don't want to type any commands:
- The `prod-demo-agent` container runs this exact sequence automatically every few seconds in the background.

---

## 4. What to Look For (Expected Result)

### In the Terminal:
You will receive an HTTP `200 OK` response with a JSON-RPC payload containing sample support tickets:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tickets": [
      {"id": "TCK-1001", "title": "Cannot reset password", "status": "open"},
      {"id": "TCK-1002", "title": "Billing invoice discrepancy", "status": "open"}
    ]
  }
}
```

### In the Admin Console UI:
1. Go to **`https://localhost:8444/audit`**.
2. Notice the top row of the table:
   - **Decision:** Green badge displaying **`ALLOW`**.
   - **Tool:** `support-desk-mcp/list_tickets`.
   - **Principal:** `agent-reader`.
3. Click the row to expand the details:
   - **Risk Level:** `read`.
   - **Policy Version:** `v1.0.0`.
   - **Audit Chain Hash:** Displays the SHA-256 cryptographic link tying this record to previous events.

---

## 5. What Happened Behind the Scenes?
1. The agent sent its request with its Keycloak identity token to the Gateway (`port 3443`).
2. Gateway intercepted the request via Envoy `ext_authz` and called AgentGate (`port 9443`).
3. AgentGate extracted the tool name (`list_tickets`), checked the tool catalog, saw risk was `read`.
4. Cedar evaluated the rule: `permit(principal in Role::"support-reader", action, resource);` ➔ **ALLOW**.
5. AgentGate minted a short-lived RS256 JWT, stripped the agent's inbound token, injected the newly minted token, and forwarded the request to `support-desk-mcp`.
6. An immutable audit record was written to PostgreSQL.
