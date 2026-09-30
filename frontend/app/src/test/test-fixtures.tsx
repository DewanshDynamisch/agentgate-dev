import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../state/ToastProvider';
import type {
  ActivateResponse,
  DryRunCompareResponse,
  PolicyRecord,
  RollbackResponse,
  ValidateResponse,
} from '@contract/models/governance';
import type { GovernanceClient } from '@contract/api/governanceClient';

export function renderWithRouter(ui: ReactNode, initialEntries = ['/']) {
  return (
    <MemoryRouter initialEntries={initialEntries}>
      <ToastProvider>{ui}</ToastProvider>
    </MemoryRouter>
  );
}

export function policy(version: string, state: PolicyRecord['state']): PolicyRecord {
  return {
    workspace_id: 'default',
    version,
    content: 'permit(principal, action, resource);',
    state,
    description: `${state} policy`,
    created_at: '2026-09-18T12:00:00Z',
    activated_at: state === 'active' ? '2026-09-18T12:00:00Z' : undefined,
  };
}

export function governanceState(policies: PolicyRecord[]) {
  return {
    status: 'idle' as const,
    workspaceId: 'default',
    policies,
    activeVersion: policies.find((item) => item.state === 'active')?.version,
    activationStatus: 'idle' as const,
    dryRunResult: undefined,
    rollbackStatus: 'idle' as const,
    lastError: undefined,
  };
}

export function clientWithWorkflow(overrides: Partial<GovernanceClient> = {}): GovernanceClient {
  const active = policy('active-v1', 'active');
  const candidate = policy('candidate-v2', 'candidate');
  let policies = [active, candidate];
  return {
    listPolicies: async () => policies,
    validatePolicy: async (): Promise<ValidateResponse> => ({ valid: true, version: 'candidate-v2' }),
    createCandidate: async () => candidate,
    activatePolicy: async (): Promise<ActivateResponse> => {
      policies = [policy('active-v1', 'historical'), policy('candidate-v2', 'active')];
      return { workspace_id: 'default', active_version: 'candidate-v2', previous_version: 'active-v1', activated_at: '2026-09-18T12:00:00Z' };
    },
    rollbackPolicy: async (): Promise<RollbackResponse> => {
      policies = [policy('active-v1', 'active'), policy('candidate-v2', 'historical')];
      return { workspace_id: 'default', active_version: 'active-v1', rolled_back_from: 'candidate-v2', activated_at: '2026-09-18T12:00:00Z' };
    },
    previewPolicy: async (): Promise<{ version: string; results: never[] }> => ({ version: 'candidate-v2', results: [] }),
    dryRunCompare: async (): Promise<DryRunCompareResponse> => ({ candidate_version: 'candidate-v2', results: [] }),
    listAuditEvents: async (workspaceId: string) => ({ workspaceId, events: [] }),
    listTools: async () => [],
    upsertTool: async () => { throw new Error('upsertTool not stubbed'); },
    deleteTool: async () => undefined,
    ...overrides,
  };
}
