# Operations

AgentGate provides a robust operational foundation for managing and observing production deployments.

## Production Topology

The standard AgentGate production topology is a single-node deployment utilizing Docker Compose. It consists of the core AgentGate service, PostgreSQL for policy and audit storage, an Envoy-based API Gateway, the Admin UI, and Keycloak for identity management.

## TLS and mTLS

Production deployments enforce strict encryption:
- Internal services communicate over mTLS, with a 4096-bit internal Certificate Authority.
- AgentGate includes automatic zero-downtime hot reloading for server TLS certificates. When new certificates are written, they are picked up after the 1-minute cache TTL expires without requiring a process restart.

## Metrics

AgentGate exposes Prometheus metrics on the internal port `:9090` at `/metrics`.
Key metrics to alert on include:
- `agentgate_audit_append_failures_total`: Audit write failures.
- `agentgate_credential_issuance_failures_total`: Downstream credential minting failures.
- `agentgate_decisions_total`: The volume of ALLOW and DENY decisions.
- `agentgate_decision_duration_seconds`: Authorization evaluation latency.

## Secrets Handling

Cryptographically strong secrets and certificates are initialized automatically. The `.env` file maps these secrets to the respective containers, and files are protected using Docker secrets (`0644` permissions for container user access).

## Restart, Backup, and Restore

- **Restarting Services:** The AgentGate daemon and associated services are managed via Docker Compose standard commands.
- **Backup:** Use `pg_dump` to create consistent binary backups of the PostgreSQL database without downtime.
- **Restore:** Use `pg_restore` to recreate the schema and data. After any restore, always run the cryptographic audit chain verification to guarantee log integrity.

## Key Rotation Limitation

AgentGate's JWKS endpoint currently publishes only a single active RSA public key. Rotating this key requires pausing inbound traffic, replacing the key, and restarting AgentGate and any downstream MCP servers.

> Verified by `docs/OPERATIONS/RUNBOOK.md`.
