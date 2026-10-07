import { beforeEach, describe, expect, it, vi } from "vitest";

const mgr = vi.hoisted(() => ({
  signinRedirect: vi.fn(async () => undefined),
  signoutRedirect: vi.fn(async () => undefined),
  signinRedirectCallback: vi.fn(),
  getUser: vi.fn(),
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

function setConfig(c: Record<string, string>) {
  (window as unknown as Record<string, unknown>).__AGENTGATE_CONFIG__ = c;
}

beforeEach(() => {
  vi.clearAllMocks();
  resetOidcForTests();
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
});
