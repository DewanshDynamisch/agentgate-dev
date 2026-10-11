import { describe, expect, it, vi } from "vitest";
import { createHttpGovernanceClient } from "./governanceHttp";

const json = { "Content-Type": "application/json" };

describe("createHttpGovernanceClient", () => {
  it("sends the bearer token from getAccessToken", async () => {
    const fetchMock = vi.fn(async () => new Response("[]", { status: 200, headers: json }));
    const client = createHttpGovernanceClient("http://api", async () => "tok-1", vi.fn(), fetchMock as unknown as typeof fetch);
    await client.listPolicies("default");
    const init = (fetchMock.mock.calls[0] as unknown[])[1] as RequestInit;
    expect(new Headers(init.headers).get("Authorization")).toBe("Bearer tok-1");
  });

  it("calls onUnauthorized when the API returns 401", async () => {
    const fetchMock = vi.fn(
      async () => new Response(JSON.stringify({ error: { code: "UNAUTHORIZED", message: "no" } }), { status: 401, headers: json })
    );
    const onUnauthorized = vi.fn();
    const client = createHttpGovernanceClient("http://api", async () => "t", onUnauthorized, fetchMock as unknown as typeof fetch);
    await expect(client.listPolicies("default")).rejects.toMatchObject({ status: 401 });
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it("calls onUnauthorized when no access token is available", async () => {
    const onUnauthorized = vi.fn();
    const client = createHttpGovernanceClient(
      "http://api",
      async () => {
        throw new Error("Not authenticated");
      },
      onUnauthorized,
      vi.fn() as unknown as typeof fetch
    );
    await expect(client.listPolicies("default")).rejects.toThrow();
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });
});
