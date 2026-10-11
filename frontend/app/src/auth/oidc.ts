import { UserManager, WebStorageStateStore, type User } from "oidc-client-ts";
import { getConfig } from "./config";

export const ADMIN_ROLE = "agentgate-admin";

export interface AppUser {
  username: string;
  roles: string[];
}

let manager: UserManager | null = null;

export function isSsoConfigured(): boolean {
  const c = getConfig();
  return c.oidcAuthority !== "" && c.oidcClientId !== "";
}

/** Test hook: drop the cached UserManager so config changes take effect. */
export function resetOidcForTests(): void {
  manager = null;
}

function getManager(): UserManager {
  if (manager) return manager;
  if (!isSsoConfigured()) {
    throw new Error("SSO not configured: oidcAuthority and oidcClientId are required");
  }
  const c = getConfig();
  manager = new UserManager({
    authority: c.oidcAuthority,
    client_id: c.oidcClientId,
    redirect_uri: `${window.location.origin}/auth/callback`,
    post_logout_redirect_uri: window.location.origin,
    response_type: "code",
    scope: c.oidcScope,
    // Renewal uses the refresh_token grant (oidc-client-ts v3 signinSilent does
    // this whenever the stored user has a refresh_token, which Keycloak's code
    // flow returns by default), so no iframe/silent_redirect_uri page is needed.
    // If the IdP issues no refresh token, renewal is not attempted and the
    // 401 -> login() path restarts SSO.
    automaticSilentRenew: true,
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
  });
  singleFlightSilentRenew(manager);
  return manager;
}

/**
 * Make every silent renewal (the library's automatic renewal and our own
 * getAccessToken) share ONE in-flight refresh. With refresh-token rotation /
 * reuse detection a second concurrent refresh would fail invalid_grant and
 * bounce the user to login. Cleared on settle so a later call can retry.
 */
function singleFlightSilentRenew(m: UserManager): void {
  const original = m.signinSilent.bind(m);
  let inflight: Promise<User | null> | null = null;
  m.signinSilent = (...args: Parameters<UserManager["signinSilent"]>) => {
    if (!inflight) {
      inflight = original(...args).finally(() => {
        inflight = null;
      });
    }
    return inflight;
  };
}

/** Subscribe to user (re)loads such as silent renewals; returns an unsubscribe. */
export function onUserLoaded(cb: (user: AppUser) => void): () => void {
  if (!isSsoConfigured()) return () => undefined;
  const events = getManager().events;
  const handler = (u: User) => cb(toAppUser(u));
  events.addUserLoaded(handler);
  return () => events.removeUserLoaded(handler);
}

function stringArray(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

function decodeJwtPayload(token: unknown): Record<string, unknown> {
  if (typeof token !== "string") return {};
  try {
    const seg = token.split(".")[1];
    if (!seg) return {};
    const b64 = seg.replace(/-/g, "+").replace(/_/g, "/");
    const json = decodeURIComponent(
      Array.from(atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4)), (c) =>
        "%" + c.charCodeAt(0).toString(16).padStart(2, "0")
      ).join("")
    );
    const parsed: unknown = JSON.parse(json);
    return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function rolesFrom(claims: Record<string, unknown>): string[] {
  const realm = (claims.realm_access as { roles?: unknown } | undefined)?.roles;
  return [...stringArray(claims.roles), ...stringArray(realm)];
}

/**
 * Display-only mapping for the UI gate. Roles come from the access token
 * payload (Keycloak puts realm_access.roles there), decoded WITHOUT
 * verification (the backend verifies the token and enforces roles), falling
 * back to the ID-token profile claims. Malformed tokens yield no roles.
 */
export function toAppUser(user: Pick<User, "profile"> & { access_token?: string }): AppUser {
  const p = user.profile as Record<string, unknown>;
  const username =
    (typeof p.preferred_username === "string" && p.preferred_username) ||
    (typeof p.email === "string" && p.email) ||
    (typeof p.sub === "string" && p.sub) ||
    "unknown";
  const roles = new Set([...rolesFrom(decodeJwtPayload(user.access_token)), ...rolesFrom(p)]);
  return { username, roles: [...roles] };
}

export async function login(): Promise<void> {
  await getManager().signinRedirect();
}

export async function logout(): Promise<void> {
  const m = getManager();
  try {
    await m.signoutRedirect();
  } catch {
    // IdP without end_session_endpoint etc.: the local session must not survive.
    await m.removeUser();
  }
}

/** Resolves the current non-expired access token; throws when unauthenticated. */
export async function getAccessToken(): Promise<string> {
  const m = getManager();
  let user = await m.getUser();
  if (user?.expired && user.refresh_token) {
    try {
      user = await m.signinSilent();
    } catch {
      user = null;
    }
  }
  if (!user || user.expired || !user.access_token) {
    throw new Error("Not authenticated");
  }
  return user.access_token;
}

export async function handleCallback(): Promise<AppUser> {
  return toAppUser(await getManager().signinRedirectCallback());
}

/** The signed-in user from session storage, or null (also null when SSO is unconfigured). */
export async function getCurrentUser(): Promise<AppUser | null> {
  if (!isSsoConfigured()) return null;
  const user = await getManager().getUser();
  return user && !user.expired ? toAppUser(user) : null;
}
