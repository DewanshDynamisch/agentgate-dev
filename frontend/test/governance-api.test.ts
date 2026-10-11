import { describe, expect, it } from "vitest";
import {
  GovernanceApiError,
  PolicyRecord,
  ValidateResponse,
  ActivateResponse,
  RollbackResponse,
} from "../src/models/governance.js";
import {
  MockGovernanceClient,
  HttpGovernanceClient,
  staticToken,
} from "../src/api/governanceClient.js";
import { g3Fixtures } from "../src/fixtures/governanceFixtures.js";

describe("Frontend G3 Governance API Client & Models", () => {
  it("MockGovernanceClient supports full lifecycle workflow", async () => {
    const client = new MockGovernanceClient();
    const ws = "workspace-mock";

    // 1. Initially empty
    const initialList = await client.listPolicies(ws);
    expect(initialList).toHaveLength(0);

    // 2. Validate valid policy
    const valValid = await client.validatePolicy(ws, g3Fixtures.validPolicy1);
    expect(valValid.valid).toBe(true);
    expect(valValid.version).toBeDefined();

    // 3. Validate invalid policy
    const valInvalid = await client.validatePolicy(ws, g3Fixtures.invalidPolicy);
    expect(valInvalid.valid).toBe(false);
    expect(valInvalid.errors).toBeDefined();
    expect(valInvalid.errors?.length).toBeGreaterThan(0);

    // 4. Create candidate
    const c1 = await client.createCandidate(ws, g3Fixtures.validPolicy1, "v1 permit");
    expect(c1.state).toBe("candidate");
    expect(c1.workspace_id).toBe(ws);

    // 5. Activate candidate
    const act1 = await client.activatePolicy(ws, c1.version);
    expect(act1.active_version).toBe(c1.version);
    expect(act1.previous_version).toBeUndefined();

    // Verify state in list
    const listAfterAct1 = await client.listPolicies(ws);
    expect(listAfterAct1).toHaveLength(1);
    expect(listAfterAct1[0]?.state).toBe("active");

    // 6. Create second candidate and activate
    const c2 = await client.createCandidate(ws, g3Fixtures.validPolicy2, "v2 forbid");
    const act2 = await client.activatePolicy(ws, c2.version);
    expect(act2.active_version).toBe(c2.version);
    expect(act2.previous_version).toBe(c1.version);

    // Verify c1 is now historical
    const listAfterAct2 = await client.listPolicies(ws);
    const p1 = listAfterAct2.find((p) => p.version === c1.version);
    const p2 = listAfterAct2.find((p) => p.version === c2.version);
    expect(p1).toBeDefined();
    expect(p2).toBeDefined();
    expect(p1?.state).toBe("historical");
    expect(p2?.state).toBe("active");

    // 7. Rollback to c1
    const rb = await client.rollbackPolicy(ws, c1.version);
    expect(rb.active_version).toBe(c1.version);
    expect(rb.rolled_back_from).toBe(c2.version);

    // 8. Rollback to unknown version throws GovernanceApiError
    await expect(client.rollbackPolicy(ws, "unknown-hash")).rejects.toThrow(GovernanceApiError);

    // 9. Preview against c2
    const preview = await client.previewPolicy(ws, c2.version, [
      {
        principal_id: "agent-1",
        principal_roles: ["reader"],
        resource_id: "tool-read",
        resource_risk: "read",
      },
    ]);
    expect(preview.version).toBe(c2.version);
    expect(preview.results).toHaveLength(1);
  });

  it("HttpGovernanceClient handles HTTP requests and errors cleanly", async () => {
    let capturedHeaders: Record<string, string> = {};
    let capturedMethod = "";
    let capturedUrl = "";

    const mockFetch = async (url: string, init?: { method?: string; headers?: Record<string, string>; body?: string }) => {
      capturedUrl = url;
      capturedMethod = init?.method || "GET";
      capturedHeaders = init?.headers || {};

      if (capturedUrl.endsWith("/policies") && capturedMethod === "GET") {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            workspace_id: "ws-test",
            policies: [
              {
                workspace_id: "ws-test",
                version: "hash-123",
                content: "permit(principal, action, resource);",
                state: "active" as const,
                description: "Test",
                created_at: "2026-09-13T10:00:00Z",
              },
            ],
          }),
        };
      }

      if (capturedUrl.includes("rollback") && init?.body?.includes("bad-hash")) {
        return {
          ok: false,
          status: 404,
          json: async () => ({
            error: {
              code: "NOT_FOUND",
              message: "target policy version not found",
            },
          }),
        };
      }

      return {
        ok: true,
        status: 200,
        json: async () => ({}),
      };
    };

    const client = new HttpGovernanceClient("http://localhost:8090", staticToken("test-admin-token"), mockFetch as any);

    const list = await client.listPolicies("ws-test");
    expect(capturedUrl).toBe("http://localhost:8090/api/v1/workspaces/ws-test/policies");
    expect(capturedMethod).toBe("GET");
    expect(capturedHeaders["Authorization"]).toBe("Bearer test-admin-token");
    expect(list).toHaveLength(1);
    expect(list[0]?.version).toBe("hash-123");

    // Test error mapping
    await expect(client.rollbackPolicy("ws-test", "bad-hash")).rejects.toThrow(GovernanceApiError);
  });
});
