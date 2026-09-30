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
  HttpGovernanceClient,
  staticToken,
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
  const apiUrl = import.meta.env.VITE_API_URL || "";
  const adminToken = import.meta.env.VITE_ADMIN_TOKEN || "";
  const useMock = import.meta.env.VITE_USE_MOCK === "true" || !adminToken;
  const client: GovernanceClient = useMemo(
    () => (useMock ? new MockGovernanceClient() : new HttpGovernanceClient(apiUrl, staticToken(adminToken))),
    [adminToken, apiUrl, useMock]
  );
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
      if (useMock) {
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
