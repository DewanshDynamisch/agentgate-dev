import { beforeEach, describe, expect, it, vi } from "vitest";

const mgr = vi.hoisted(() => ({
  signinRedirect: vi.fn(async () => undefined),
  signoutRedirect: vi.fn(async () => undefined),
  signinRedirectCallback: vi.fn(),
  getUser: vi.fn(),
  signinSilent: vi.fn(),
  removeUser: vi.fn(async () => undefined),
  events: { addUserLoaded: vi.fn(), removeUserLoaded: vi.fn() },
}));
const ctor = vi.hoisted(() => vi.fn());
vi.mock("oidc-client-ts", () => ({
  UserManager: class {
    constructor(settings: unknown) {
      ctor(settings);
      return mgr as never;
    }
  },
  WebStorageStateStore: class {
    opts: unknown;
    constructor(opts: unknown) {
      this.opts = opts;
    }
  },
}));

import { getAccessToken, handleCallback, login, logout, resetOidcForTests, toAppUser } from "./oidc";

const silent = vi.fn();

function setConfig(c: Record<string, string>) {
  (window as unknown as Record<string, unknown>).__AGENTGATE_CONFIG__ = c;
}

beforeEach(() => {
  vi.clearAllMocks();
  resetOidcForTests();
  mgr.signinSilent = silent;
  setConfig({ apiUrl: "", oidcAuthority: "https://idp.example/realms/ag", oidcClientId: "agentgate-ui", oidcScope: "" });
});

describe("oidc", () => {
  it("builds a PKCE code-flow manager with session storage and silent renew", async () => {
    await login();
    const settings = ctor.mock.calls[0][0] as Record<string, unknown>;
    expect(settings.authority).toBe("https://idp.example/realms/ag");
    expect(settings.client_id).toBe("agentgate-ui");
    expect(settings.redirect_uri).toBe(`${window.location.origin}/auth/callback`);
    expect(settings.post_logout_redirect_uri).toBe(window.location.origin);
    expect(settings.response_type).toBe("code");
    expect(settings.scope).toBe("openid profile");
    expect(settings.automaticSilentRenew).toBe(true);
    expect(settings.userStore).toBeDefined();
    expect(mgr.signinRedirect).toHaveBeenCalled();
  });

  it("logout redirects through the IdP", async () => {
    await logout();
    expect(mgr.signoutRedirect).toHaveBeenCalled();
  });

  it("getAccessToken returns the token and throws when unauthenticated", async () => {
    mgr.getUser.mockResolvedValueOnce({ access_token: "tok", expired: false });
    await expect(getAccessToken()).resolves.toBe("tok");
    mgr.getUser.mockResolvedValueOnce(null);
    await expect(getAccessToken()).rejects.toThrow(/not authenticated/i);
    mgr.getUser.mockResolvedValueOnce({ access_token: "old", expired: true });
    await expect(getAccessToken()).rejects.toThrow(/not authenticated/i);
  });

  it("handleCallback completes the redirect and returns the app user", async () => {
    mgr.signinRedirectCallback.mockResolvedValueOnce({
      profile: { preferred_username: "alice", roles: ["agentgate-admin"] },
    });
    await expect(handleCallback()).resolves.toEqual({ username: "alice", roles: ["agentgate-admin"] });
  });

  it("throws a clear error when SSO is not configured", async () => {
    setConfig({ oidcAuthority: "", oidcClientId: "" });
    resetOidcForTests();
    await expect(login()).rejects.toThrow(/not configured/i);
  });

  it("toAppUser reads roles from roles and realm_access claims", () => {
    expect(
      toAppUser({ profile: { sub: "s", realm_access: { roles: ["a"] }, roles: ["b"] } } as never)
    ).toEqual({ username: "s", roles: ["b", "a"] });
    expect(toAppUser({ profile: { sub: "s" } } as never).roles).toEqual([]);
  });

  it("renews an expired token via the refresh token (no iframe) before giving up", async () => {
    mgr.getUser.mockResolvedValueOnce({ access_token: "old", expired: true, refresh_token: "rt" });
    silent.mockResolvedValueOnce({ access_token: "new", expired: false });
    await expect(getAccessToken()).resolves.toBe("new");
    expect(silent).toHaveBeenCalledTimes(1);
  });

  it("does not attempt renewal without a refresh token, and fails closed if renewal fails", async () => {
    mgr.getUser.mockResolvedValueOnce({ access_token: "old", expired: true });
    await expect(getAccessToken()).rejects.toThrow(/not authenticated/i);
    expect(silent).not.toHaveBeenCalled();
    mgr.getUser.mockResolvedValueOnce({ access_token: "old", expired: true, refresh_token: "rt" });
    silent.mockRejectedValueOnce(new Error("invalid_grant"));
    await expect(getAccessToken()).rejects.toThrow(/not authenticated/i);
  });

  it("logout removes the local session even if the IdP redirect throws", async () => {
    mgr.signoutRedirect.mockRejectedValueOnce(new Error("no end_session_endpoint"));
    await logout();
    expect(mgr.removeUser).toHaveBeenCalled();
  });

  describe("roles for the UI gate", () => {
    const jwt = (payload: unknown) =>
      `h.${btoa(JSON.stringify(payload)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")}.s`;

    it("reads realm_access.roles from the access token", () => {
      const u = { access_token: jwt({ realm_access: { roles: ["agentgate-admin"] } }), profile: { sub: "s" } };
      expect(toAppUser(u as never).roles).toEqual(["agentgate-admin"]);
    });
    it("reads roles from the profile when the access token has none", () => {
      const u = { access_token: jwt({}), profile: { sub: "s", roles: ["agentgate-admin"] } };
      expect(toAppUser(u as never).roles).toEqual(["agentgate-admin"]);
    });
    it("yields no roles when neither has any", () => {
      expect(toAppUser({ access_token: jwt({}), profile: { sub: "s" } } as never).roles).toEqual([]);
    });
    it("does not crash on a malformed access token", () => {
      for (const t of ["garbage", "a.%%%.c", "a.bm90anNvbg.c", "", undefined]) {
        expect(toAppUser({ access_token: t, profile: { sub: "s" } } as never).roles).toEqual([]);
      }
    });
  });

  describe("single-flight renewal", () => {
    const expired = { access_token: "old", expired: true, refresh_token: "rt" };

    it("5 concurrent callers trigger exactly one renewal and all get the new token", async () => {
      mgr.getUser.mockResolvedValue(expired);
      let resolve!: (u: unknown) => void;
      silent.mockReturnValue(new Promise((r) => (resolve = r)));
      const calls = Array.from({ length: 5 }, () => getAccessToken());
      await new Promise((r) => setTimeout(r, 10));
      resolve({ access_token: "new", expired: false });
      await expect(Promise.all(calls)).resolves.toEqual(Array(5).fill("new"));
      expect(silent).toHaveBeenCalledTimes(1);
    });

    it("a failing renewal rejects all callers and a later call can retry", async () => {
      mgr.getUser.mockResolvedValue(expired);
      silent.mockRejectedValueOnce(new Error("invalid_grant"));
      const results = await Promise.allSettled(Array.from({ length: 5 }, () => getAccessToken()));
      expect(results.every((r) => r.status === "rejected")).toBe(true);
      expect(silent).toHaveBeenCalledTimes(1);
      silent.mockResolvedValueOnce({ access_token: "new", expired: false });
      await expect(getAccessToken()).resolves.toBe("new");
      expect(silent).toHaveBeenCalledTimes(2);
    });

    it("library-driven renewals share the same in-flight call", async () => {
      mgr.getUser.mockResolvedValue(expired);
      let resolve!: (u: unknown) => void;
      silent.mockReturnValue(new Promise((r) => (resolve = r)));
      await login(); // builds and patches the manager
      const lib = mgr.signinSilent();
      const mine = getAccessToken();
      await new Promise((r) => setTimeout(r, 10));
      resolve({ access_token: "new", expired: false });
      await Promise.all([lib, mine]);
      expect(silent).toHaveBeenCalledTimes(1);
    });
  });
});
