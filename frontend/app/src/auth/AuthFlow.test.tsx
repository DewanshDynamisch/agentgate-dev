import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";

const oidc = vi.hoisted(() => ({
  ADMIN_ROLE: "agentgate-admin",
  login: vi.fn(async () => undefined),
  logout: vi.fn(async () => undefined),
  handleCallback: vi.fn(),
  getCurrentUser: vi.fn(),
  isSsoConfigured: vi.fn(() => true),
  getAccessToken: vi.fn(),
  onUserLoaded: vi.fn(() => () => undefined),
}));
vi.mock("./oidc", () => oidc);

import { AuthProvider } from "../state/AuthProvider";
import { RequireAuth } from "../components/auth/RequireAuth";
import { LoginPage } from "../pages/Login/LoginPage";
import { AuthCallbackPage } from "../pages/AuthCallback";

function app(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/auth/callback" element={<AuthCallbackPage />} />
          <Route path="/" element={<RequireAuth><div>secret console</div></RequireAuth>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
}

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  oidc.isSsoConfigured.mockReturnValue(true);
  oidc.getCurrentUser.mockResolvedValue(null);
});

describe("OIDC auth flow", () => {
  it("redirects unauthenticated users to Login", async () => {
    app("/");
    expect(await screen.findByRole("button", { name: /sign in with sso/i })).toBeInTheDocument();
    expect(screen.queryByText("secret console")).toBeNull();
  });

  it("Login button starts the SSO redirect", async () => {
    app("/login");
    await userEvent.click(await screen.findByRole("button", { name: /sign in with sso/i }));
    expect(oidc.login).toHaveBeenCalled();
  });

  it("shows an SSO not configured state instead of crashing", async () => {
    oidc.isSsoConfigured.mockReturnValue(false);
    app("/login");
    expect(await screen.findByText(/sso not configured/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /sign in with sso/i })).toBeNull();
  });

  it("callback stores the user and navigates to /", async () => {
    oidc.handleCallback.mockResolvedValue({ username: "alice", roles: ["agentgate-admin"] });
    app("/auth/callback?code=abc&state=xyz");
    expect(await screen.findByText("secret console")).toBeInTheDocument();
    expect(oidc.handleCallback).toHaveBeenCalledTimes(1);
  });

  it("users without agentgate-admin see not authorized", async () => {
    oidc.getCurrentUser.mockResolvedValue({ username: "bob", roles: ["viewer"] });
    app("/");
    expect(await screen.findByText(/not authorized/i)).toBeInTheDocument();
    expect(screen.queryByText("secret console")).toBeNull();
  });

  it("admin users reach the console", async () => {
    oidc.getCurrentUser.mockResolvedValue({ username: "alice", roles: ["agentgate-admin"] });
    app("/");
    expect(await screen.findByText("secret console")).toBeInTheDocument();
  });

  it("callback failure shows an error", async () => {
    oidc.handleCallback.mockRejectedValue(new Error("bad state"));
    app("/auth/callback?code=abc");
    await waitFor(() => expect(screen.getByText(/sign-in failed/i)).toBeInTheDocument());
  });

  it("pushes a renewed user into state", async () => {
    let cb: (u: unknown) => void = () => undefined;
    oidc.onUserLoaded.mockImplementation(((f: (u: unknown) => void) => {
      cb = f;
      return () => undefined;
    }) as never);
    oidc.getCurrentUser.mockResolvedValue({ username: "alice", roles: ["viewer"] });
    app("/");
    expect(await screen.findByText(/not authorized/i)).toBeInTheDocument();
    cb({ username: "alice", roles: ["agentgate-admin"] });
    expect(await screen.findByText("secret console")).toBeInTheDocument();
  });
});
