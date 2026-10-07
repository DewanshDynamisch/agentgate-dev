import type { AuditEvent } from "@contract/models/audit";

export interface HourBucket {
  hourStartIso: string;
  allowed: number;
  denied: number;
}

export interface AuditStats {
  total: number;
  allowed: number;
  denied: number;
  denyRate: number;
  /** Deny reasons only (reason of DENY decisions) -> count. */
  byReason: Record<string, number>;
  topTools: { tool: string; count: number }[];
  last24hBuckets: HourBucket[];
}

export interface ObservedIdentity {
  agentId: string;
  roles: string[];
  lastSeen: string;
  allowed: number;
  denied: number;
}

const HOUR_MS = 3_600_000;
const TOP_N = 5;

/** Mutation events are not authorization decisions and are excluded everywhere. */
export function isDecision(e: AuditEvent): boolean {
  return e.eventType === "decision";
}

export function toolKey(e: AuditEvent): string {
  return e.toolBackendId || e.toolName ? `${e.toolBackendId}/${e.toolName}` : "";
}

export function computeStats(events: AuditEvent[], now: Date): AuditStats {
  const decisions = events.filter(isDecision);
  const currentHour = Math.floor(now.getTime() / HOUR_MS) * HOUR_MS;
  const windowStart = currentHour - 23 * HOUR_MS;
  const buckets: HourBucket[] = Array.from({ length: 24 }, (_, i) => ({
    hourStartIso: new Date(windowStart + i * HOUR_MS).toISOString(),
    allowed: 0,
    denied: 0,
  }));

  let allowed = 0;
  let denied = 0;
  const byReason: Record<string, number> = {};
  const toolCounts = new Map<string, number>();

  for (const e of decisions) {
    const isAllow = e.decision === "ALLOW";
    const isDeny = e.decision === "DENY";
    if (isAllow) allowed += 1;
    if (isDeny) {
      denied += 1;
      const reason = e.reason || "unspecified";
      byReason[reason] = (byReason[reason] ?? 0) + 1;
    }
    const tool = toolKey(e);
    if (tool) toolCounts.set(tool, (toolCounts.get(tool) ?? 0) + 1);

    const t = new Date(e.timestamp).getTime();
    if (!Number.isNaN(t) && t >= windowStart && t < currentHour + HOUR_MS) {
      const b = buckets[Math.floor((t - windowStart) / HOUR_MS)];
      if (b) {
        if (isAllow) b.allowed += 1;
        if (isDeny) b.denied += 1;
      }
    }
  }

  const topTools = [...toolCounts.entries()]
    .map(([tool, count]) => ({ tool, count }))
    .sort((a, b) => b.count - a.count || a.tool.localeCompare(b.tool))
    .slice(0, TOP_N);

  const total = decisions.length;
  return {
    total,
    allowed,
    denied,
    denyRate: total === 0 ? 0 : denied / total,
    byReason,
    topTools,
    last24hBuckets: buckets,
  };
}

/** Distinct agents seen in decision events, most recently seen first. */
export function observedIdentities(events: AuditEvent[]): ObservedIdentity[] {
  const map = new Map<string, ObservedIdentity & { roleSet: Set<string> }>();
  for (const e of events) {
    if (!isDecision(e) || !e.principalAgentId) continue;
    let row = map.get(e.principalAgentId);
    if (!row) {
      row = { agentId: e.principalAgentId, roles: [], roleSet: new Set(), lastSeen: e.timestamp, allowed: 0, denied: 0 };
      map.set(e.principalAgentId, row);
    }
    for (const r of e.principalRoles ?? []) row.roleSet.add(r);
    if (e.decision === "ALLOW") row.allowed += 1;
    if (e.decision === "DENY") row.denied += 1;
    if (new Date(e.timestamp).getTime() > new Date(row.lastSeen).getTime()) row.lastSeen = e.timestamp;
  }
  return [...map.values()]
    .map(({ roleSet, ...row }) => ({ ...row, roles: [...roleSet].sort() }))
    .sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime() || a.agentId.localeCompare(b.agentId));
}
