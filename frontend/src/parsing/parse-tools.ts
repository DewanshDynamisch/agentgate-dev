import type { ApiError } from "../models/api-error.js";
import type { ToolArgument, ToolDefinition } from "../models/tools.js";
import type { ParseResult } from "./parse-authorization-result.js";

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function invalid(message: string): { ok: false; error: ApiError } {
  return { ok: false, error: { kind: "invalid_response", message } };
}

export function parseToolDefinition(raw: unknown): ParseResult<ToolDefinition> {
  if (!isRecord(raw)) return invalid("tool definition is not an object");
  const req: Record<string, string> = {};
  for (const k of ["backend_id", "tool_name", "risk"]) {
    const v = raw[k];
    if (typeof v !== "string" || v === "") return invalid(`tool definition missing ${k}`);
    req[k] = v;
  }
  const opt: Record<string, string> = {};
  for (const k of ["description", "updated_at", "updated_by"]) {
    const v = raw[k];
    if (v === undefined || v === null) {
      opt[k] = "";
    } else if (typeof v === "string") {
      opt[k] = v;
    } else {
      return invalid(`tool definition field ${k} must be a string`);
    }
  }
  if (raw["schema"] === undefined || raw["schema"] === null) return invalid("tool definition missing schema");

  const args: ToolArgument[] = [];
  const argsRaw = raw["arguments"];
  if (argsRaw !== undefined && argsRaw !== null) {
    if (!Array.isArray(argsRaw)) return invalid("tool definition arguments must be an array");
    for (const a of argsRaw) {
      if (!isRecord(a) || typeof a["name"] !== "string" || typeof a["type"] !== "string" || typeof a["required"] !== "boolean") {
        return invalid("tool definition has a malformed argument declaration");
      }
      args.push({ name: a["name"], type: a["type"], required: a["required"] });
    }
  }

  return {
    ok: true,
    value: {
      backendId: req["backend_id"]!,
      toolName: req["tool_name"]!,
      risk: req["risk"]!,
      schema: raw["schema"],
      arguments: args,
      description: opt["description"]!,
      updatedAt: opt["updated_at"]!,
      updatedBy: opt["updated_by"]!,
    },
  };
}

export function parseToolDefinitions(raw: unknown): ParseResult<ToolDefinition[]> {
  if (!isRecord(raw) || !Array.isArray(raw["tools"])) return invalid("tools response missing tools array");
  const out: ToolDefinition[] = [];
  for (const t of raw["tools"]) {
    const p = parseToolDefinition(t);
    if (!p.ok) return p;
    out.push(p.value);
  }
  return { ok: true, value: out };
}
