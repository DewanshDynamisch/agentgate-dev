export interface AgentGateConfig {
  apiUrl: string;
  oidcAuthority: string;
  oidcClientId: string;
  oidcScope: string;
}

declare global {
  interface Window {
    __AGENTGATE_CONFIG__?: Partial<AgentGateConfig>;
  }
}

/**
 * Runtime config from /config.js (window.__AGENTGATE_CONFIG__). Falls back to
 * the non-secret VITE_API_URL dev value for apiUrl only; OIDC values have no
 * fallback so an unconfigured deployment surfaces "SSO not configured".
 */
export function getConfig(): AgentGateConfig {
  const raw = (typeof window !== "undefined" ? window.__AGENTGATE_CONFIG__ : undefined) ?? {};
  const s = (v: unknown) => (typeof v === "string" ? v.trim() : "");
  return {
    apiUrl: s(raw.apiUrl) || (import.meta.env.VITE_API_URL as string | undefined) || "",
    oidcAuthority: s(raw.oidcAuthority),
    oidcClientId: s(raw.oidcClientId),
    oidcScope: s(raw.oidcScope) || "openid profile",
  };
}
