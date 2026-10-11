# Scenario 5: Policy Lifecycle (Dry-Run & Instant Rollback)

## 1. What This Proves (What to Say)
> "Deploying a broken authorization policy in production can take down critical business workflows. AgentGate treats policies with the rigor of code: policies are versioned, candidates can be tested against historical audit logs using 'Dry-Run Simulation' to measure counterfactual impact, and if an issue occurs, operators can atomically roll back with a single click."

---

## 2. Pre-Conditions
- Admin Console is open at **`https://localhost:8444/policies`**.

---

## 3. Step-by-Step Execution

### Step 1: Inspect the Active Policy
1. Navigate to **`https://localhost:8444/policies`** in your browser.
2. Under the **Active Policy** card, notice:
   - Version: **`v1.0.0`**
   - Active status with a green badge.
3. Review the declarative Cedar policy syntax shown in the editor/viewer.

---

### Step 2: Create a Candidate Policy
1. Click the **"Create Candidate"** button.
2. In the candidate policy dialog:
   - Provide a description: `Stricter read-only lockdown for weekend maintenance`.
   - In the Cedar editor, add or update a policy statement (e.g. adding a restrictive clause).
3. Click **Create Candidate**.
4. The candidate appears in the table under version `v1.1.0` (or next sequence).

---

### Step 3: Run Dry-Run Simulation (The Safety Guard)
1. Next to your candidate policy, click **Dry-Run Analysis**.
2. **What this does:** AgentGate takes the candidate Cedar code and simulates evaluating it against the last 500 real audit events recorded in your PostgreSQL database.
3. **What the screen displays:**
   - **Counterfactual Impact Summary:**
     - Total Historical Events Evaluated: `500`
     - Impact: Shows how many transactions that previously succeeded would now be **DENIED**.
   - This gives the security engineer mathematical confidence *before* activating the change.

---

### Step 4: Activate & Atomic Rollback
1. Click **Activate Policy**.
2. The UI confirms that `v1.1.0` is now the active policy serving live traffic.
3. To demonstrate disaster recovery, immediately click the **Rollback** button (or select `v1.0.0` and click Rollback).
4. The system issues an atomic database transaction restoring `v1.0.0` as the active engine baseline.

---

## 4. What to Point Out to the Audience
- **Audit continuity:** Both the activation and the rollback events are themselves recorded in the audit log.
- **Zero service restarts:** No Envoy configuration reloads, no downtime, and no drop in running gRPC connections.
