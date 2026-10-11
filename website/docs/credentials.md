# Downstream Credentials

AgentGate mints short-lived downstream credentials for every authorized tool execution, rather than forwarding the inbound agent token. The inbound token is never forwarded to downstream tools.

## The Minted Credential

The downstream credential is an RS256 JWT with the following standard and custom claims:

- **iss** (Issuer): AgentGate
- **aud** (Audience): The configured backend audience (e.g., the MCP server)
- **sub** (Subject): The caller's identity (the AI agent or user)
- **act** (Actor, optional): Included if the call is on behalf of a user
- **scope** (Scope): The permissions granted for the tool
- **tool** (Tool): The name of the tool being executed
- **exec** (Execution ID): Unique ID correlating the credential to the specific audit record
- **jti** (JWT ID): Unique token identifier used for replay prevention
- **exp** (Expiration Time): Maximum lifetime is typically 300 seconds

## JSON Web Key Set (JWKS)

AgentGate exposes a JWKS endpoint (`/.well-known/jwks.json`) containing the public RSA keys needed to verify signatures. Backends should fetch and cache these keys.

## Replay and Expiry Behavior

Backends must enforce:
- **Expiry:** Reject credentials if current time > `exp` (allowing for clock skew).
- **Replay Prevention:** Track `jti`s until their `exp`. If a `jti` is seen again, reject it as a replay.

## Verifying in Your Backend

Here is a step-by-step Go recipe to verify an AgentGate credential, using the support desk MCP backend as a reference.

```go
// 1. Bearer prefix.
const prefix = "Bearer "
if !strings.HasPrefix(authorizationHeader, prefix) {
	return Claims{}, errors.New("missing bearer credential")
}
token := strings.TrimSpace(authorizationHeader[len(prefix):])

// 2. Decode and verify RS256 header and signature using the public key from the JWKS endpoint.
// ...

// 3. Verify time bounds: not before (nbf) and expiry (exp).
if raw.Exp <= now.Add(-clockSkew).Unix() {
	return Claims{}, errors.New("credential expired")
}

// 4. Verify tool binding: ensure the credential was issued for the requested tool.
if raw.Tool != toolName {
	return Claims{}, errors.New("credential not issued for this tool")
}

// 5. Enforce single-use via JTI tracking.
if !v.consumeJTI(raw.JTI, raw.Exp, now) {
	return Claims{}, errors.New("credential replayed")
}
```

> Verified by `deploy/demo/support-desk-mcp/credverify.go`.
