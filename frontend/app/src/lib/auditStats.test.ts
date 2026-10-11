import { describe, expect, it } from 'vitest';
import { computeStats, observedIdentities } from './auditStats';
import type { AuditEvent } from '@contract/models/audit';

const NOW = new Date('2026-09-29T10:30:00Z');

let seq = 0;
function ev(over: Partial<AuditEvent>): AuditEvent {
  seq += 1;
  return {
    id: `e${seq}`, workspaceId: 'default', sequenceNumber: seq, executionId: '',
    timestamp: '2026-09-29T10:00:00Z', eventType: 'decision', decision: 'ALLOW', reason: '',
    principalAgentId: 'agent-a', principalRoles: [], principalOnBehalfOf: '',
    toolBackendId: 'crm', toolName: 'get_customer', toolRisk: 'read',
    policyVersion: 'v1', policyHash: 'h', redactedArguments: {}, ...over,
  };
}

const FIXTURE: AuditEvent[] = [
  ev({ timestamp: '2026-09-29T10:05:00Z' }),
  ev({ timestamp: '2026-09-29T10:10:00Z' }),
  ev({ timestamp: '2026-09-29T09:59:59Z', toolName: 'search' }),
  ev({ timestamp: '2026-09-29T09:00:00Z', decision: 'DENY', reason: 'no_permit', toolName: 'delete_customer' }),
  ev({ timestamp: '2026-09-29T09:30:00Z', decision: 'DENY', reason: 'no_permit', toolName: 'delete_customer' }),
  ev({ timestamp: '2026-09-29T08:00:00Z', decision: 'DENY', reason: 'unknown_tool', toolBackendId: 'x', toolName: 'y' }),
  ev({ timestamp: '2026-09-28T11:30:00Z' }),
  ev({ timestamp: '2026-09-28T10:30:00Z' }), // before the 24h window (starts 11:00 on the 28th)
  ev({ timestamp: '2026-09-27T01:00:00Z', decision: 'DENY', reason: 'no_permit' }), // outside window
  ev({ timestamp: '2026-09-29T10:20:00Z', eventType: 'mutation', decision: '' }), // excluded
];

describe('computeStats', () => {
  const s = computeStats(FIXTURE, NOW);
  it('counts only decision events', () => {
    expect(s.total).toBe(9);
    expect(s.allowed).toBe(5);
    expect(s.denied).toBe(4);
    expect(s.denyRate).toBeCloseTo(4 / 9);
  });
  it('groups deny reasons', () => {
    expect(s.byReason).toEqual({ no_permit: 3, unknown_tool: 1 });
  });
  it('ranks top tools (max 5) by count', () => {
    expect(s.topTools[0]).toEqual({ tool: 'crm/get_customer', count: 5 });
    expect(s.topTools[1]).toEqual({ tool: 'crm/delete_customer', count: 2 });
    expect(s.topTools.length).toBeLessThanOrEqual(5);
  });
  it('builds 24 hourly buckets ending at the current hour', () => {
    expect(s.last24hBuckets).toHaveLength(24);
    expect(s.last24hBuckets[23]).toEqual({ hourStartIso: '2026-09-29T10:00:00.000Z', allowed: 2, denied: 0 });
    expect(s.last24hBuckets[0]).toEqual({ hourStartIso: '2026-09-28T11:00:00.000Z', allowed: 1, denied: 0 });
    const b9 = s.last24hBuckets.find((b) => b.hourStartIso === '2026-09-29T09:00:00.000Z')!;
    expect(b9).toMatchObject({ allowed: 1, denied: 2 });
    const b8 = s.last24hBuckets.find((b) => b.hourStartIso === '2026-09-29T08:00:00.000Z')!;
    expect(b8).toMatchObject({ allowed: 0, denied: 1 });
    expect(s.last24hBuckets.reduce((n, b) => n + b.allowed + b.denied, 0)).toBe(7);
  });
  it('handles no events', () => {
    const e = computeStats([], NOW);
    expect(e).toMatchObject({ total: 0, allowed: 0, denied: 0, denyRate: 0, byReason: {}, topTools: [] });
    expect(e.last24hBuckets).toHaveLength(24);
  });
});

describe('observedIdentities', () => {
  it('derives distinct agents with roles, last seen and counts from decisions only', () => {
    const rows = observedIdentities([
      ev({ principalAgentId: 'a1', principalRoles: ['support'], timestamp: '2026-09-29T10:00:00Z' }),
      ev({ principalAgentId: 'a1', principalRoles: ['support', 'ops'], decision: 'DENY', timestamp: '2026-09-29T11:00:00Z' }),
      ev({ principalAgentId: 'a2', timestamp: '2026-09-29T09:00:00Z' }),
      ev({ principalAgentId: 'a3', eventType: 'mutation', decision: '' }),
      ev({ principalAgentId: '' }),
    ]);
    expect(rows.map((r) => r.agentId)).toEqual(['a1', 'a2']);
    expect(rows[0]).toMatchObject({ roles: ['ops', 'support'], lastSeen: '2026-09-29T11:00:00Z', allowed: 1, denied: 1 });
    expect(rows[1]).toMatchObject({ roles: [], allowed: 1, denied: 0 });
  });
});
