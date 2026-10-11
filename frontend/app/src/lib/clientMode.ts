export type ClientMode = "mock" | "http";

export interface ClientModeEnv {
  MODE?: string | undefined;
  DEV?: boolean | undefined;
  PROD?: boolean | undefined;
  VITE_USE_MOCK?: string | undefined;
}

/**
 * Pure client selection. The in-memory mock is allowed only in the test
 * environment or an explicit dev-server opt-in. A production build always uses
 * the HTTP client: VITE_USE_MOCK and MODE are ignored there (fail-closed).
 */
export function selectClientMode(env: ClientModeEnv): ClientMode {
  if (env.PROD === true) return "http";
  if (env.MODE === "test") return "mock";
  if (env.DEV === true && env.VITE_USE_MOCK === "true") return "mock";
  return "http";
}
