# Frequently Asked Questions (FAQ)

### Why am I getting a 401 Unauthorized at the AgentGateway?
This typically means the inbound JWT is expired, has a bad signature, or the wrong audience.
Inspect your token to ensure the Keycloak client has the `agentgate-gateway` audience mapper, and refresh the token if necessary.

### Why did my tool call fail with a 403 Forbidden (`policy_deny`)?
The agent's roles do not satisfy the requirements for the target tool's risk tier under the active Cedar policy.
Check the audit log to view the decision reason, and verify the policy grants the necessary access.

### Why is the backend returning a 401 Unauthorized for my tool call?
The downstream minted credential was rejected by the backend MCP server. This usually happens if the backend's JWKS cache is stale or if the signing key was rotated. Restarting the backend server will force it to fetch the latest public key.

### Why is AgentGate unhealthy or exiting immediately?
AgentGate relies on the PostgreSQL database. If the database connection is refused or a migration is pending, AgentGate will fail to start. Ensure the `prod-postgres` container is healthy and migrations have completed successfully.

### Why am I stuck in a login loop or seeing a 401 in the Admin UI?
This is often caused by a Keycloak hostname mismatch or the admin user missing the required role. Ensure the `KC_HOSTNAME` variable matches the browser's access URL, and verify the admin user has the `agentgate-admin` realm role.

### Why did `verify-audit` fail with a Hash Mismatch?
A hash mismatch indicates that an audit log row was modified out-of-band or data corruption occurred. You should investigate database write logs for unauthorized direct SQL execution and consider restoring from a known good backup.

### Why is the Envoy mTLS handshake failing?
There might be a client certificate or CA mismatch between the gateway and AgentGate. Verify that the client certificate is signed by the correct internal CA and that the gateway configuration points to the valid CA certificate.

> Verified by `docs/OPERATIONS/RUNBOOK.md`.
