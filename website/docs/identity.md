---
sidebar_position: 4
---

# Identity

AgentGate receives an inbound identity that has been validated by the gateway. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
AgentGate does not validate the raw token; it reads fields according to its configuration. Verified by agentgate/internal/identity/mapper.go.

## Keycloak Realm Roles and Agents
The production deployment uses a Keycloak realm named `agentgate`. Verified by deploy/prod/keycloak/realm-agentgate.json.
Agents authenticate using the OAuth 2.0 Client Credentials grant type. Verified by deploy/prod/keycloak/realm-agentgate.json.
The following realm roles are configured:
- `agentgate-admin`: Administrator of AgentGate governance API and UI. Verified by deploy/prod/keycloak/realm-agentgate.json.
- `reader`: Read-only agent role. Verified by deploy/prod/keycloak/realm-agentgate.json.
- `admin`: Admin agent role. Verified by deploy/prod/keycloak/realm-agentgate.json.
- `payer`: Payer agent role. Verified by deploy/prod/keycloak/realm-agentgate.json.

The following client-credentials agents are configured with service accounts:
- `agent-reader`: Mapped to the `reader` role. Verified by deploy/prod/keycloak/realm-agentgate.json.
- `agent-admin`: Mapped to the `admin` role. Verified by deploy/prod/keycloak/realm-agentgate.json.
- `agentgate-ops-cli`: Mapped to the `agentgate-admin` role for CLI automation. Verified by deploy/prod/keycloak/realm-agentgate.json.

## Required Claims and Role Mapping
AgentGate expects specific claims to be present in the gateway-validated context. Verified by agentgate/internal/identity/mapper.go.
- **Agent ID (`sub`)**: The authoritative identifier for the calling agent. Must be present. Verified by agentgate/internal/identity/mapper.go.
- **Roles (`roles`)**: A space-separated list of roles for the identity. Must be present and resolve to a non-empty set. Verified by agentgate/internal/identity/mapper.go.
- **On-behalf-of (`obo`)**: An optional claim indicating the human identity the agent is acting for. If present, it must not equal the agent ID. Verified by agentgate/internal/identity/mapper.go.

Any failure to map these claims (missing required claim, malformed claim, ambiguous identity, or missing roles) results in a closed failure (DENY). Verified by agentgate/internal/identity/mapper.go.
The gateway maps Keycloak's JSON array of roles into a space-separated `roles` string using an OIDC audience and role mapper. Verified by deploy/prod/keycloak/realm-agentgate.json.

## Trust Boundary
AgentGate is trusted only when reached through the authenticated gateway-to-AgentGate service boundary. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
A direct or unauthenticated caller to the AgentGate decision service is not a trusted request. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
AgentGate must never forward the received bearer token to a downstream MCP server. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.

## Admin Sign-in
The AgentGate Admin UI uses the standard OIDC flow (Authorization Code) to sign in human operators. Verified by deploy/prod/keycloak/realm-agentgate.json.
Admin users log in using the `admin` account, which is assigned the `agentgate-admin` realm role. Verified by deploy/prod/keycloak/realm-agentgate.json.
Administrative operations require authenticated identity and explicit authorization. Verified by docs/SECURITY/PRODUCTION-INVARIANTS.md.
