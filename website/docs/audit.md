# Audit Log

AgentGate persists every authorization decision to an append-only cryptographic audit log.

## Fields Recorded

The audit record contains the following context:
- Identity context (the caller's identity)
- Workspace
- Tool invoked
- Decision (ALLOW or DENY)
- Reason code for the decision
- Correlation/execution identifier
- Timestamp
- Exact policy version/hash evaluated

## Redaction

Arguments must not become an accidental secrets/PII store. Each governed tool has an explicit redaction mode applied before persistence.

## Cryptographic Hash Chain

Audit integrity is tamper-evident through cryptographic hash chaining and restricted application permissions. Each audit record hashes its own contents alongside the hash of the preceding record, creating a verifiable chain of custody.

## Verifying the Audit Chain

You can verify the cryptographic integrity of the audit chain using the AgentGate CLI. A successful verification confirms no records have been altered or deleted out-of-band.

```bash
docker compose -f deploy/prod/docker-compose.yml exec -T agentgate \
  agentgate verify-audit --workspace default
```

Expected output:
```text
OK <record_count> records
```

If this command exits non-zero or reports a hash mismatch, the audit sequence is compromised or corrupted.

> Verified by `docs/OPERATIONS/RUNBOOK.md` and `docs/SECURITY/PRODUCTION-INVARIANTS.md`.
