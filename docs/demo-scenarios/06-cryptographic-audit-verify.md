# Scenario 6: Cryptographic Audit Hash Verification

## 1. What This Proves (What to Say)
> "In high-stakes enterprise compliance, standard database logs are not enough because a database administrator or malicious actor with root access can edit rows after the fact. AgentGate writes every audit event into a SHA-256 cryptographic hash chain—similar to a blockchain or git tree. We provide a CLI tool that verifies the mathematical continuity of the chain."

---

## 2. Pre-Conditions
- Stack is running.
- Terminal is open in the repo root.

---

## 3. Step-by-Step Execution

Open PowerShell and run the `verify-audit` CLI command directly inside the AgentGate container:

```powershell
docker exec prod-agentgate /bin/agentgate verify-audit -workspace default
```

---

## 4. What to Look For (Expected Result)

### Terminal Output:
```text
OK <N> records
```
*(Example: `OK 48 records`)*

### What This Means:
1. AgentGate read all stored audit events in the `default` workspace sequentially from PostgreSQL.
2. For each row:
   - It recomputed the SHA-256 hash over: `(event_id, timestamp, principal, tool, risk_level, decision, arguments_hash, previous_row_hash)`.
   - It verified that `current_row.previous_hash == previous_row.current_hash`.
3. If even a single character in an argument payload, timestamp, or decision was modified by a rogue database query, the chain would break and the tool would immediately exit with a failure naming the corrupted record ID.

---

## 5. Visualizing in the UI
1. Open **`https://localhost:8444/audit`**.
2. Expand any audit event.
3. Show the **"Audit Chain"** section in the details drawer:
   - **Current Hash:** `sha256:...`
   - **Previous Hash:** `sha256:...`
4. Explain: Every single tool call we made in Scenarios 1 through 4 is immutably anchored in this sequence.
