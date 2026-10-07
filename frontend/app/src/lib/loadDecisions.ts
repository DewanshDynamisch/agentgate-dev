import type { GovernanceClient } from "@contract/api/governanceClient";
import type { AuditEvent } from "@contract/models/audit";
import { isDecision } from "./auditStats";

export const DECISION_LIMIT = 500;
const PAGE_SIZE = 100; // server maximum per page
const MAX_PAGES = 30; // safety bound if mutation events dominate

/** Pages newest-first until `limit` decision events are collected, the end is reached, or the page bound is hit. */
export async function loadRecentDecisions(
  client: GovernanceClient,
  workspaceId: string,
  limit: number = DECISION_LIMIT
): Promise<AuditEvent[]> {
  const out: AuditEvent[] = [];
  let before: number | undefined;
  for (let page = 0; page < MAX_PAGES && out.length < limit; page += 1) {
    const res = await client.listAuditEvents(workspaceId, { limit: PAGE_SIZE, beforeSequence: before });
    for (const e of res.events) {
      if (isDecision(e) && out.length < limit) out.push(e);
    }
    if (res.nextBeforeSequence === undefined || res.events.length === 0) break;
    before = res.nextBeforeSequence;
  }
  return out;
}
