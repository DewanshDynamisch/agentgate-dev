# Scenario 4: Unknown / Unregistered Tool Injection Blocked

## 1. What This Proves (What to Say)
> "Generative AI models are prone to hallucinating non-existent tools, and attackers often attempt prompt injection attacks instructing models to execute hidden commands (like `run_bash_command` or `export_database`). AgentGate enforces strict tool schema whitelisting: any tool not explicitly registered in the organizational catalog is blocked before it can do harm."

---

## 2. Pre-Conditions
- Stack is running.
- Terminal is open in the repo root.

---

## 3. Step-by-Step Execution

Open PowerShell and simulate an injected or hallucinated tool call named `dump_customer_database`:

```powershell
# 1. Obtain token for 'agent-reader'
$agentSecret = (Get-Content deploy/prod/secrets/agent_reader_secret).Trim()
$tokenResp = curl.exe -k -s -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token `
    -H "Content-Type: application/x-www-form-urlencoded" `
    -d "grant_type=client_credentials&client_id=agent-reader&client_secret=$agentSecret"
$agentToken = ($tokenResp | ConvertFrom-Json).access_token

# 2. Call an unregistered / hallucinated tool
curl.exe -k -s -i -X POST https://localhost:3443/ `
    -H "Authorization: Bearer $agentToken" `
    -H "Content-Type: application/json" `
    -d '{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"tools/call\",\"params\":{\"name\":\"dump_customer_database\",\"arguments\":{\"format\":\"csv\"}}}'
```

---

## 4. What to Look For (Expected Result)

### In the Terminal:
You receive an immediate HTTP `403 Forbidden` response indicating an unknown tool:
```http
HTTP/2 403
content-type: application/json

{"error":"access_denied","reason":"unknown_tool","message":"tool 'dump_customer_database' is not registered in the tool catalog"}
```

### In the Admin Console UI:
1. Go to **`https://localhost:8444/audit`**.
2. Filter by **`DENY`**.
3. View the latest rejection:
   - **Decision:** **`DENY`**.
   - **Tool Name:** `dump_customer_database`.
   - **Reason:** `unknown_tool`.
4. Click to expand:
   - Notice that the full argument payload `{"format":"csv"}` is securely captured in the audit log, giving security analysts immediate visibility into prompt injection or abnormal model behavior.

---

## 5. What Happened Behind the Scenes?
1. Before invoking the Cedar policy engine, AgentGate's Catalog Guard looks up the requested tool in its PostgreSQL registry.
2. The catalog uses canonical SHA-256 schema fingerprints. Because `dump_customer_database` is not registered, AgentGate refuses to route the call.
3. This is an essential **fail-closed** invariant: by default, unknown actions are denied.
