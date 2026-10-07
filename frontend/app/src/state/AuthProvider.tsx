import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import * as oidc from "../auth/oidc";
import type { AppUser } from "../auth/oidc";

interface AuthContextValue {
  user: AppUser | null;
  /** True until the stored session has been read. */
  loading: boolean;
  ssoConfigured: boolean;
  login: () => Promise<void>;
  logout: () => Promise<void>;
  /** Completes the IdP redirect (callback route) and stores the user. */
  completeLogin: () => Promise<AppUser>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AppUser | null>(null);
  const [loading, setLoading] = useState(true);
  const callbackRef = useRef<Promise<AppUser> | null>(null);

  useEffect(() => {
    let cancelled = false;
    oidc
      .getCurrentUser()
      // Never overwrite a user already set by the callback exchange.
      .then((u) => !cancelled && setUser((prev) => prev ?? u))
      .catch(() => undefined)
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => oidc.onUserLoaded((u) => setUser(u)), []);

  const completeLogin = useCallback(() => {
    // The authorization code is single-use: share one in-flight exchange (StrictMode double effects).
    if (!callbackRef.current) {
      callbackRef.current = oidc.handleCallback().then((u) => {
        setUser(u);
        return u;
      });
    }
    return callbackRef.current;
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      loading,
      ssoConfigured: oidc.isSsoConfigured(),
      login: () => oidc.login(),
      logout: async () => {
        setUser(null);
        await oidc.logout();
      },
      completeLogin,
    }),
    [user, loading, completeLogin]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth() must be used inside <AuthProvider>");
  }
  return ctx;
}
