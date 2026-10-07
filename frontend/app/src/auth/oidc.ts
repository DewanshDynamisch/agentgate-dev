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
    automaticSilentRenew: true,
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
  });
  return manager;
}

function stringArray(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

/** Display-only mapping. The backend re-verifies the token and enforces roles. */
export function toAppUser(user: Pick<User, "profile">): AppUser {
  const p = user.profile as Record<string, unknown>;
  const realm = (p.realm_access as { roles?: unknown } | undefined)?.roles;
  const username =
    (typeof p.preferred_username === "string" && p.preferred_username) ||
    (typeof p.email === "string" && p.email) ||
    (typeof p.sub === "string" && p.sub) ||
    "unknown";
  return { username, roles: [...stringArray(p.roles), ...stringArray(realm)] };
}

export async function login(): Promise<void> {
  await getManager().signinRedirect();
}

export async function logout(): Promise<void> {
  await getManager().signoutRedirect();
}

/** Resolves the current non-expired access token; throws when unauthenticated. */
export async function getAccessToken(): Promise<string> {
  const user = await getManager().getUser();
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
