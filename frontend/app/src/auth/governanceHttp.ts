import { HttpGovernanceClient, type GovernanceClient } from "@contract/api/governanceClient";
import { GovernanceApiError } from "@contract/models/governance";

/**
 * HTTP governance client authenticated with a per-request OIDC bearer token.
 * A 401 from the API, or the absence of a usable token, invokes
 * onUnauthorized (which starts the SSO login); the original error still
 * propagates so callers fail closed.
 */
export function createHttpGovernanceClient(
  apiUrl: string,
  getAccessToken: () => Promise<string>,
  onUnauthorized: () => void,
  fetchImpl?: typeof fetch
): GovernanceClient {
  const tokenProvider = async () => {
    try {
      return await getAccessToken();
    } catch (err) {
      onUnauthorized();
      throw err;
    }
  };
  const inner = new HttpGovernanceClient(apiUrl, tokenProvider, fetchImpl);
  return new Proxy(inner, {
    get(target, prop, receiver) {
      const value = Reflect.get(target, prop, receiver);
      if (typeof value !== "function") return value;
      return async (...args: unknown[]) => {
        try {
          return await (value as (...a: unknown[]) => unknown).apply(target, args);
        } catch (err) {
          if (err instanceof GovernanceApiError && err.status === 401) onUnauthorized();
          throw err;
        }
      };
    },
  });
}
