# Scenario 7: Public Portal & Offline Attack Player

## 1. What This Proves (What to Say)
> "AgentGate comes equipped with a developer portal and documentation website that operates 100% offline with zero external CDN dependencies, making it suitable for air-gapped secure enterprise enclaves. It also includes an interactive 'Attack Scenarios Player' driven directly by the repository's security test suite."

---

## 2. Pre-Conditions
1. The documentation website is being served. If not already running, in a PowerShell terminal:
   ```powershell
   cd website
   npm run serve -- --port 3000
   ```
2. Open **`http://localhost:3000`** in your browser.

---

## 3. Step-by-Step Execution

### Step 1: The Request-Flow Animation
1. Scroll down to the **"How it works"** section on the homepage.
2. Observe the animated request token traveling:
   - `Agent` ➔ `AgentGateway` ➔ `AgentGate (Cedar Policy Engine)`.
3. Point out the two alternating paths:
   - **Green path (Allowed):** AgentGate mints an ephemeral JWT and forwards to the Tool.
   - **Red path (Denied):** The request is immediately rejected at AgentGate and never reaches the Tool.
4. Show the **Pause** button and explain that the animation automatically complies with accessibility standards (`prefers-reduced-motion`).

---

### Step 2: The Attack Scenarios Player
1. Scroll to the **"See It In Action: Attack Scenarios Player"** (`#see-it-block`).
2. This is an interactive stepper loaded with 8 real scenarios mapped directly to tests in `agentgate/qa/v1security/security_gate_test.go`.
3. Click the **Next** / **Previous** buttons (or use keyboard arrow keys):
   - **Scenario: Identity Spoofing (`TestV1_07_IdentitySpoofing`):**
     - Step 1: Rogue agent presents forged JWT claim claiming to be `admin-operator`.
     - Step 2: AgentGate validates Keycloak cryptographic signature on `sub` and `obo`.
     - Step 3: Signature validation fails ➔ **Verdict: BLOCKED (Status: 403)**.
   - **Scenario: Replayed Credential (`TestV1_03_CredentialReplay`):**
     - Attacker captures an ephemeral token and attempts to reuse it outside its 60-second window.
     - AgentGate rejects the replayed JTI ➔ **Verdict: BLOCKED (Status: 401)**.
   - **Scenario: Audit Log Tampering (`TestV1_15_AuditTamper`):**
     - Insider attempts direct UPDATE on PostgreSQL audit rows.
     - Append-only trigger rejects modification ➔ **Verdict: BLOCKED (Integrity enforced)**.

---

### Step 3: Offline Self-Contained Documentation
1. Click **Docs** in the navbar.
2. In the search bar, type `token` or `policy`.
3. Notice that results populate instantly: search is computed entirely locally using client-side index slices (no Algolia or external search calls).
4. Point out the API Reference (`/docs/api`) automatically generated from the OpenAPI specification.
