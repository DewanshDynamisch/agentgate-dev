import { describe, expect, it } from "vitest";
import { GovernanceApiError } from "../src/models/governance.js";
import { HttpGovernanceClient, MockGovernanceClient, staticToken } from "../src/api/governanceClient.js";

interface Captured { url: string; method: string; headers: Record<string, string>; body?: string | undefined }

function fakeFetch(respond: (c: Captured) => { status: number; body?: unknown }) {
  const calls: Captured[] = [];
  const fn = async (url: string, init?: { method?: string; headers?: Record<string, string>; body?: string }) => {
    const c: Captured = { url, method: init?.method ?? "GET", headers: init?.headers ?? {}, body: init?.body };
    calls.push(c);
    const r = respond(c);
    return { ok: r.status >= 200 && r.status < 300, status: r.status, json: async () => r.body };
  };
  return { fn, calls };
}

const wireEvent = {
  id: "e1", workspace_id: "default", sequence_number: 11, execution_id: "x1",
  timestamp: "2026-09-29T10:00:00Z", event_type: "authorization_decision", decision: "ALLOW",
  reason: "policy_allow", principal_agent_id: "agent-1", principal_roles: ["ops"],
  principal_on_behalf_of: "", tool_backend_id: "b", tool_name: "t", tool_risk: "read",
  policy_version: "v1", policy_hash: "h", redacted_arguments: { a: "[REDACTED]" },
};

const wireTool = {
  backend_id: "b", tool_name: "t", risk: "read", schema: { type: "object" },
  arguments: [{ name: "a", type: "string", required: true }], description: "d",
  updated_at: "2026-09-29T10:00:00Z", updated_by: "admin",
};

describe("audit events client", () => {
  it("builds the query and parses into camelCase", async () => {
    const f = fakeFetch(() => ({ status: 200, body: { workspace_id: "default", events: [wireEvent], next_before_sequence: 11 } }));
    const c = new HttpGovernanceClient("http://h/", staticToken("tok"), f.fn as any);
    const page = await c.listAuditEvents("default", { limit: 50, beforeSequence: 10 });
    expect(f.calls[0]!.url).toBe("http://h/api/v1/workspaces/default/audit-events?limit=50&before_sequence=10");
    expect(page.nextBeforeSequence).toBe(11);
    expect(page.events[0]).toMatchObject({ sequenceNumber: 11, eventType: "authorization_decision", principalAgentId: "agent-1", toolBackendId: "b", redactedArguments: { a: "[REDACTED]" } });
  });

  it("tolerates empty strings on mutation events", async () => {
    const ev = { sequence_number: 1, timestamp: "t", event_type: "tool_upserted", decision: "", principal_agent_id: "" };
    const f = fakeFetch(() => ({ status: 200, body: { workspace_id: "default", events: [ev] } }));
    const c = new HttpGovernanceClient("http://h", staticToken("tok"), f.fn as any);
    const page = await c.listAuditEvents("default");
    expect(f.calls[0]!.url).toBe("http://h/api/v1/workspaces/default/audit-events");
    expect(page.events[0]!.decision).toBe("");
    expect(page.nextBeforeSequence).toBeUndefined();
  });

  it("rejects an event missing sequence_number", async () => {
    const { sequence_number: _s, ...bad } = wireEvent;
    const f = fakeFetch(() => ({ status: 200, body: { workspace_id: "default", events: [bad] } }));
    const c = new HttpGovernanceClient("http://h", staticToken("tok"), f.fn as any);
    await expect(c.listAuditEvents("default")).rejects.toThrow(GovernanceApiError);
  });

  it("maps 401 to GovernanceApiError UNAUTHORIZED", async () => {
    const f = fakeFetch(() => ({ status: 401, body: { error: { code: "UNAUTHORIZED", message: "no" } } }));
    const c = new HttpGovernanceClient("http://h", staticToken("tok"), f.fn as any);
    const err = await c.listAuditEvents("default").catch((e) => e);
    expect(err).toBeInstanceOf(GovernanceApiError);
    expect(err.status).toBe(401);
    expect(err.code).toBe("UNAUTHORIZED");
  });
});

describe("tool catalog client", () => {
  it("upsertTool PUTs a JSON body with a provider bearer token", async () => {
    const f = fakeFetch(() => ({ status: 200, body: wireTool }));
    let n = 0;
    const c = new HttpGovernanceClient("http://h", async () => `tok-${++n}`, f.fn as any);
    const out = await c.upsertTool("b", "t", { risk: "read", schema: { type: "object" }, arguments: [{ name: "a", type: "string", required: true }], description: "d" });
    const call = f.calls[0]!;
    expect(call.method).toBe("PUT");
    expect(call.url).toBe("http://h/api/v1/tools/b/t");
    expect(call.headers["Authorization"]).toBe("Bearer tok-1");
    expect(JSON.parse(call.body!)).toEqual({ risk: "read", schema: { type: "object" }, arguments: [{ name: "a", type: "string", required: true }], description: "d" });
    expect(out.backendId).toBe("b");
    expect(out.arguments[0]!.required).toBe(true);
  });

  it("listTools parses and rejects malformed entries", async () => {
    const ok = fakeFetch(() => ({ status: 200, body: { tools: [wireTool] } }));
    expect((await new HttpGovernanceClient("http://h", staticToken("t"), ok.fn as any).listTools())[0]!.toolName).toBe("t");
    const bad = fakeFetch(() => ({ status: 200, body: { tools: [{ ...wireTool, risk: undefined }] } }));
    await expect(new HttpGovernanceClient("http://h", staticToken("t"), bad.fn as any).listTools()).rejects.toThrow(GovernanceApiError);
  });

  it("deleteTool handles 204 and 404", async () => {
    const f = fakeFetch((c) => (c.url.endsWith("/gone") ? { status: 404, body: { error: { code: "NOT_FOUND", message: "tool not found" } } } : { status: 204 }));
    const c = new HttpGovernanceClient("http://h", staticToken("t"), f.fn as any);
    await expect(c.deleteTool("b", "t")).resolves.toBeUndefined();
    expect(f.calls[0]!.method).toBe("DELETE");
    await expect(c.deleteTool("b", "gone")).rejects.toMatchObject({ status: 404, code: "NOT_FOUND" });
  });
});

describe("MockGovernanceClient catalog and audit", () => {
  it("supports tool CRUD and audit paging", async () => {
    const m = new MockGovernanceClient();
    await m.upsertTool("b", "t", { risk: "read", schema: {}, arguments: [], description: "" });
    expect(await m.listTools()).toHaveLength(1);
    await m.deleteTool("b", "t");
    await expect(m.deleteTool("b", "t")).rejects.toThrow(GovernanceApiError);
    expect((await m.listAuditEvents("w")).events).toHaveLength(0);
  });
});
