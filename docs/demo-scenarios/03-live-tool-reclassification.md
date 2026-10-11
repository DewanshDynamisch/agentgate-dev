# Scenario 3: Live Tool Risk Reclassification (Dynamic Governance)

## 1. What This Proves (What to Say)
> "In traditional architectures, changing an API endpoint's security classification requires editing code, changing config maps, and redeploying or restarting microservices. With AgentGate, security teams can reclassify tool risk in real time through the console. Changes take effect on the very next incoming packet without interrupting running services."

---

## 2. Pre-Conditions
- Admin Console is open at `https://localhost:8444/tools`.
- Stack is running.

---

## 3. Step-by-Step Execution

### Step 1: Observe the Initial State (Allowed Call)
In terminal, test calling `close_ticket` as `agent-reader`. By default, `close_ticket` is classified as `write` (which the reader agent is currently allowed or configured to run depending on baseline policy):

```powershell
$agentSecret = (Get-Content deploy/prod/secrets/agent_reader_client_secret).Trim()
$tokenResp = curl.exe -k -s -X POST https://localhost:8443/realms/agentgate/protocol/openid-connect/token `
    -H "Content-Type: application/x-www-form-urlencoded" `
    -d "grant_type=client_credentials&client_id=agent-reader&client_secret=$agentSecret"
$agentToken = ($tokenResp | ConvertFrom-Json).access_token

$payload = '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"close_ticket","arguments":{"ticket_id":"TCK-1002","reason":"Resolved"}}}'
curl.exe -k -s -i -X POST https://localhost:3443/ `
    -H "Authorization: Bearer $agentToken" `
    -H "Content-Type: application/json" `
    -d $payload
```
*(Notice: If close_ticket is read/write in baseline, it succeeds or behaves per baseline policy).*

---

### Step 2: Reclassify the Tool Live in the UI
Now switch to your browser:
1. Go to **`https://localhost:8444/tools`** (Tools & Resources).
2. Look at the `support-desk-mcp` section.
3. Locate the tool named **`close_ticket`**.
4. Click the **Edit** button next to it.
5. In the dialog:
   - Change the **Risk level** dropdown to **`destructive`**.
   - A warning appears: *"Warning: Changing risk level to destructive modifies authorization boundaries"*.
   - Check the confirmation box: **"I understand this changes authorization for all agents"**.
   - Click **Save**.
6. The table refreshes immediately showing `close_ticket` with a red **`destructive`** badge.

---

### Step 3: Trigger the Tool Call Again (Immediate Denial)
Return to your PowerShell terminal and run the exact same command from Step 1 again:

```powershell
curl.exe -k -s -i -X POST https://localhost:3443/ `
    -H "Authorization: Bearer $agentToken" `
    -H "Content-Type: application/json" `
    -d $payload
```

---

## 4. What to Look For (Expected Result)

### In the Terminal:
Instead of any prior status, you now immediately receive:
```http
HTTP/2 403
content-type: application/json

{"error":"access_denied","reason":"policy_denied","message":"tool classified as destructive; principal lacks required destructive permissions"}
```

### In the Admin Console UI:
1. Navigate to **Audit** (`https://localhost:8444/audit`).
2. The latest event shows:
   - **Decision:** **`DENY`**.
   - **Tool:** `close_ticket`.
   - **Evaluated Risk Level:** **`destructive`**.
3. **Point out to audience:** We did not restart the gateway, we did not redeploy the backend MCP container, and we did not touch any code. The control plane immediately enforced the new risk boundary.

---

## 5. Clean-Up for Next Scenarios
You can either leave it as destructive, or click **Edit** on `close_ticket` in `https://localhost:8444/tools` and restore it back to `write` or run `.\deploy\prod\scripts\demo-mode.ps1 -Reset` later.
