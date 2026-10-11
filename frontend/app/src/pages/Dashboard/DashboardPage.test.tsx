import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DashboardPage } from './DashboardPage';
import { useGovernance, useGovernanceClient } from '../../state/GovernanceProvider';
import { clientWithWorkflow, governanceState, policy, renderWithRouter } from '../../test/test-fixtures';
import type { AuditEvent } from '@contract/models/audit';

vi.mock('../../state/GovernanceProvider', () => ({
  useGovernanceClient: vi.fn(),
  useGovernance: vi.fn(),
}));

function ev(seq: number, over: Partial<AuditEvent> = {}): AuditEvent {
  return {
    id: `e${seq}`, workspaceId: 'default', sequenceNumber: seq, executionId: '',
    timestamp: new Date().toISOString(), eventType: 'decision', decision: 'ALLOW', reason: '',
    principalAgentId: 'agent-a', principalRoles: [], principalOnBehalfOf: '',
    toolBackendId: 'crm', toolName: 'get_customer', toolRisk: 'read',
    policyVersion: 'v1', policyHash: 'h', redactedArguments: {}, ...over,
  };
}

function setup(events: AuditEvent[], overrides = {}) {
  const listAuditEvents = vi.fn().mockImplementation(async (_ws: string, opts: { limit?: number; beforeSequence?: number } = {}) => {
    const sorted = [...events].sort((a, b) => b.sequenceNumber - a.sequenceNumber);
    const before = opts.beforeSequence;
    const f = before === undefined ? sorted : sorted.filter((e) => e.sequenceNumber < before);
    const limit = opts.limit ?? 50;
    const page = f.slice(0, limit);
    return { workspaceId: 'default', events: page, nextBeforeSequence: f.length > limit ? page[page.length - 1]!.sequenceNumber : undefined };
  });
  const listTools = vi.fn().mockResolvedValue([{ backendId: 'crm', toolName: 'a' }, { backendId: 'crm', toolName: 'b' }]);
  const client = clientWithWorkflow({ listAuditEvents, listTools, ...overrides });
  vi.mocked(useGovernanceClient).mockReturnValue({ client, workspaceId: 'default' });
  vi.mocked(useGovernance).mockReturnValue({
    state: governanceState([policy('active-v1', 'active')]), ready: true,
  } as unknown as ReturnType<typeof useGovernance>);
  render(renderWithRouter(<DashboardPage />));
  return { listAuditEvents };
}

describe('DashboardPage', () => {
  afterEach(cleanup);

  it('renders stat values computed from audit decisions, and the label', async () => {
    const events = [
      ev(1), ev(2), ev(3),
      ev(4, { decision: 'DENY', reason: 'no_permit', toolName: 'delete_customer' }),
      ev(5, { eventType: 'mutation', decision: '' }),
    ];
    setup(events);
    expect(await screen.findByTestId('stat-total')).toHaveTextContent('4');
    expect(screen.getByTestId('stat-allowed')).toHaveTextContent('3');
    expect(screen.getByTestId('stat-denied')).toHaveTextContent('1');
    expect(screen.getByTestId('stat-deny-rate')).toHaveTextContent('25%');
    expect(screen.getByText(/last 500 decisions/i)).toBeInTheDocument();
    expect(screen.getByText('no_permit')).toBeInTheDocument();
    expect(screen.getAllByText('crm/get_customer').length).toBeGreaterThan(0);
    await waitFor(() => expect(screen.getByTestId('stat-tools')).toHaveTextContent('2'));
    expect(screen.getByTestId('stat-active-version')).toHaveTextContent('active-v1');
  });

  it('pages until 500 decisions and no further', async () => {
    const events = Array.from({ length: 650 }, (_, i) => ev(i + 1));
    const { listAuditEvents } = setup(events);
    expect(await screen.findByTestId('stat-total')).toHaveTextContent('500');
    expect(listAuditEvents).toHaveBeenCalledTimes(5);
  });

  it('shows an error when loading fails', async () => {
    setup([], { listAuditEvents: vi.fn().mockRejectedValue(new Error('audit down')) });
    expect(await screen.findByRole('alert')).toHaveTextContent('audit down');
  });

  it('shows an empty state with no decisions', async () => {
    setup([]);
    expect(await screen.findByText(/no decisions recorded yet/i)).toBeInTheDocument();
  });
});
