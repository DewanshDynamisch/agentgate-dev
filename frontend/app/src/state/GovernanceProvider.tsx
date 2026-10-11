import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  MockGovernanceClient,
  type GovernanceClient,
} from "@contract/api/governanceClient";
import { PolicyLifecycleStore, type PolicyLifecycleState } from "@contract/state/policyLifecycleState";
import type {
  ActivateResponse,
  DryRunCompareResponse,
  DryRunSample,
  PolicyRecord,
  PreviewSample,
  RollbackResponse,
  ValidateResponse,
} from "@contract/models/governance";
import { seedDemoPolicies } from "./seedDemoData";
import { selectClientMode } from "../lib/clientMode";
import { getConfig } from "../auth/config";
import { getAccessToken } from "../auth/oidc";
import { createHttpGovernanceClient } from "../auth/governanceHttp";
import { useAuth } from "./AuthProvider";
import { shouldReauth } from "./reauth";



const WORKSPACE_ID = "default";

interface GovernanceContextValue {
  state: PolicyLifecycleState;
  ready: boolean;
  loadPolicies: () => Promise<void>;
  validateDraft: (content: string) => Promise<ValidateResponse>;
  submitCandidate: (content: string, description: string) => Promise<PolicyRecord>;
  activate: (version: string) => Promise<ActivateResponse>;
  rollback: (targetVersion: string) => Promise<RollbackResponse>;
  dryRunCompare: (
    version: string,
    sampleRequests: (PreviewSample | DryRunSample)[]
  ) => Promise<DryRunCompareResponse>;
}

const GovernanceContext = createContext<GovernanceContextValue | null>(null);

interface GovernanceClientContextValue {
  client: GovernanceClient;
  workspaceId: string;
}

const GovernanceClientContext = createContext<GovernanceClientContextValue | null>(null);

/**
 * Owns one PolicyLifecycleStore instance (from the existing, tested contract
 * layer at frontend/src) for the lifetime of the app. The store class itself
 * is a plain object with no pub/sub — every action here calls the store,
 * then copies its fresh getState() snapshot into React state, so components
 * re-render automatically after every server-confirmed change. Components
 * never call the store directly.
 */
export function GovernanceProvider({ children }: { children: ReactNode }) {
  const apiUrl = getConfig().apiUrl;
  const { login } = useAuth();
  const loginRef = useRef(login);
  loginRef.current = login;
  // Mock only in tests or an explicit dev opt-in; production builds always use HTTP.
  const useMock = selectClientMode(import.meta.env) === "mock";
  const client: GovernanceClient = useMemo(() => {
    if (useMock) return new MockGovernanceClient();
    // A 401 restarts SSO. Rate-limited so a token the API keeps rejecting
    // (e.g. missing role) cannot cause an endless redirect loop.
    const onUnauthorized = () => {
      if (!shouldReauth()) return;
      void loginRef.current().catch(() => undefined);
    };
    return createHttpGovernanceClient(apiUrl, getAccessToken, onUnauthorized);
  }, [apiUrl, useMock]);
  const storeRef = useRef<PolicyLifecycleStore>(new PolicyLifecycleStore(client, WORKSPACE_ID));
  const [state, setState] = useState<PolicyLifecycleState>(storeRef.current.getState());
  const [ready, setReady] = useState(false);

  const sync = () => setState(storeRef.current.getState());

  const loadPolicies = async () => {
    await storeRef.current.loadPolicies();
    sync();
  };

  useEffect(() => {
    const initialize = async () => {
      if (useMock && !import.meta.env.PROD) {
        await seedDemoPolicies(client, WORKSPACE_ID);
      }
      await loadPolicies();
      setReady(true);
    };

    void initialize();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, useMock]);

  const value: GovernanceContextValue = {
    state,
    ready,
    loadPolicies,
    validateDraft: async (content) => {
      const res = await storeRef.current.validateDraft(content);
      sync();
      return res;
    },
    submitCandidate: async (content, description) => {
      const res = await storeRef.current.submitCandidate(content, description);
      sync();
      return res;
    },
    activate: async (version) => {
      const res = await storeRef.current.activate(version);
      sync();
      return res;
    },
    rollback: async (targetVersion) => {
      const res = await storeRef.current.rollback(targetVersion);
      sync();
      return res;
    },
    dryRunCompare: async (version, sampleRequests) => {
      const res = await storeRef.current.dryRunCompare(version, sampleRequests);
      sync();
      return res;
    },
  };

  const clientValue = useMemo(() => ({ client, workspaceId: WORKSPACE_ID }), [client]);

  return (
    <GovernanceClientContext.Provider value={clientValue}>
      <GovernanceContext.Provider value={value}>{children}</GovernanceContext.Provider>
    </GovernanceClientContext.Provider>
  );
}

export function useGovernance(): GovernanceContextValue {
  const ctx = useContext(GovernanceContext);
  if (!ctx) {
    throw new Error("useGovernance() must be used inside <GovernanceProvider>");
  }
  return ctx;
}

/** The provider-owned GovernanceClient and workspace, for pages that read data the policy store does not hold. */
export function useGovernanceClient(): GovernanceClientContextValue {
  const ctx = useContext(GovernanceClientContext);
  if (!ctx) {
    throw new Error("useGovernanceClient() must be used inside <GovernanceProvider>");
  }
  return ctx;
}
