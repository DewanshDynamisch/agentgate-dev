/**
 * Audit event models mirroring govapi.AuditEventView (agentgate/internal/govapi).
 * Wire is snake_case; these models are camelCase. Mutation events may leave
 * decision/principal/tool fields as empty strings.
 */
export interface AuditEvent {
  id: string;
  workspaceId: string;
  sequenceNumber: number;
  executionId: string;
  timestamp: string;
  eventType: string;
  decision: string;
  reason: string;
  principalAgentId: string;
  principalRoles: string[];
  principalOnBehalfOf: string;
  toolBackendId: string;
  toolName: string;
  toolRisk: string;
  policyVersion: string;
  policyHash: string;
  redactedArguments: Record<string, string>;
}

export interface AuditEventsPage {
  workspaceId: string;
  events: AuditEvent[];
  nextBeforeSequence?: number | undefined;
}

export interface ListAuditEventsOptions {
  limit?: number | undefined;
  beforeSequence?: number | undefined;
}
