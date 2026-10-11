const REAUTH_KEY = "agentgate.reauth-at";
const REAUTH_MIN_INTERVAL_MS = 30_000;

/**
 * Rate limit for 401-triggered login so a token the API keeps rejecting
 * (e.g. missing role) cannot cause an endless redirect loop.
 */
export function shouldReauth(now: number = Date.now()): boolean {
  try {
    const last = Number(sessionStorage.getItem(REAUTH_KEY) ?? "0");
    if (now - last < REAUTH_MIN_INTERVAL_MS) return false;
    sessionStorage.setItem(REAUTH_KEY, String(now));
  } catch {
    // storage unavailable: still attempt login
  }
  return true;
}
