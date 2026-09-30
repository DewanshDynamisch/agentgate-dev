import type { ApiError } from "../models/api-error.js";
import type { AuditEvent, AuditEventsPage } from "../models/audit.js";
import type { ParseResult } from "./parse-authorization-result.js";

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function invalid(message: string): { ok: false; error: ApiError } {
  return { ok: false, error: { kind: "invalid_response", message } };
}

// Optional string: absent/null -> "", present must be a string.
function optStr(o: Record<string, unknown>, k: string): string | null {
  const v = o[k];
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : null;
}

export function parseAuditEvent(raw: unknown): ParseResult<AuditEvent> {
  if (!isRecord(raw)) return invalid("audit event is not an object");
  const seq = raw["sequence_number"];
  if (typeof seq !== "number" || !Number.isFinite(seq)) return invalid("audit event missing sequence_number");
  const ts = raw["timestamp"];
  if (typeof ts !== "string" || ts === "") return invalid("audit event missing timestamp");
  const et = raw["event_type"];
  if (typeof et !== "string" || et === "") return invalid("audit event missing event_type");

  const strs: Record<string, string> = {};
  for (const k of [
    "id", "workspace_id", "execution_id", "decision", "reason", "principal_agent_id",
    "principal_on_behalf_of", "tool_backend_id", "tool_name", "tool_risk", "policy_version", "policy_hash",
  ]) {
    const v = optStr(raw, k);
    if (v === null) return invalid(`audit event field ${k} must be a string`);
    strs[k] = v;
  }

  const rolesRaw = raw["principal_roles"];
  let roles: string[] = [];
  if (rolesRaw !== undefined && rolesRaw !== null) {
    if (!Array.isArray(rolesRaw) || !rolesRaw.every((r) => typeof r === "string")) {
      return invalid("audit event principal_roles must be a string array");
    }
    roles = rolesRaw as string[];
  }

  const args: Record<string, string> = {};
  const argsRaw = raw["redacted_arguments"];
  if (argsRaw !== undefined && argsRaw !== null) {
    if (!isRecord(argsRaw)) return invalid("audit event redacted_arguments must be an object");
    for (const [k, v] of Object.entries(argsRaw)) {
      if (typeof v !== "string") return invalid(`audit event redacted_arguments.${k} must be a string`);
      args[k] = v;
    }
  }

  return {
    ok: true,
    value: {
      id: strs["id"]!,
      workspaceId: strs["workspace_id"]!,
      sequenceNumber: seq,
      executionId: strs["execution_id"]!,
      timestamp: ts,
      eventType: et,
      decision: strs["decision"]!,
      reason: strs["reason"]!,
      principalAgentId: strs["principal_agent_id"]!,
      principalRoles: roles,
      principalOnBehalfOf: strs["principal_on_behalf_of"]!,
      toolBackendId: strs["tool_backend_id"]!,
      toolName: strs["tool_name"]!,
      toolRisk: strs["tool_risk"]!,
      policyVersion: strs["policy_version"]!,
      policyHash: strs["policy_hash"]!,
      redactedArguments: args,
    },
  };
}

export function parseAuditEventsPage(raw: unknown): ParseResult<AuditEventsPage> {
  if (!isRecord(raw)) return invalid("audit events response is not an object");
  const ws = raw["workspace_id"];
  if (typeof ws !== "string") return invalid("audit events response missing workspace_id");
  const evs = raw["events"];
  if (!Array.isArray(evs)) return invalid("audit events response missing events array");
  const events: AuditEvent[] = [];
  for (const e of evs) {
    const p = parseAuditEvent(e);
    if (!p.ok) return p;
    events.push(p.value);
  }
  const next = raw["next_before_sequence"];
  if (next !== undefined && next !== null && (typeof next !== "number" || !Number.isFinite(next))) {
    return invalid("audit events response next_before_sequence must be a number");
  }
  const page: AuditEventsPage = { workspaceId: ws, events };
  if (typeof next === "number") page.nextBeforeSequence = next;
  return { ok: true, value: page };
}
