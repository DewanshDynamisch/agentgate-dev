import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { IdentitiesPage } from './IdentitiesPage';
import { useGovernanceClient } from '../../state/GovernanceProvider';
import { clientWithWorkflow } from '../../test/test-fixtures';
import type { AuditEvent } from '@contract/models/audit';

vi.mock('../../state/GovernanceProvider', () => ({ useGovernanceClient: vi.fn() }));

function ev(seq: number, over: Partial<AuditEvent>): AuditEvent {
  return {
    id: `e${seq}`, workspaceId: 'default', sequenceNumber: seq, executionId: '',
    timestamp: '2026-09-29T10:00:00Z', eventType: 'decision', decision: 'ALLOW', reason: '',
    principalAgentId: 'agent-a', principalRoles: [], principalOnBehalfOf: '',
    toolBackendId: 'crm', toolName: 't', toolRisk: 'read', policyVersion: 'v1', policyHash: 'h',
    redactedArguments: {}, ...over,
  };
}

function setup(events: AuditEvent[], listOverride?: ReturnType<typeof vi.fn>) {
  const listAuditEvents = listOverride ?? vi.fn().mockResolvedValue({ workspaceId: 'default', events });
  vi.mocked(useGovernanceClient).mockReturnValue({ client: clientWithWorkflow({ listAuditEvents }), workspaceId: 'default' });
  render(<IdentitiesPage />);
}

describe('IdentitiesPage', () => {
  afterEach(cleanup);

  it('shows observed identities derived from audit events', async () => {
    setup([
      ev(1, { principalAgentId: 'agent-support', principalRoles: ['support-agents'], principalOnBehalfOf: 'user-1' }),
      ev(2, { principalAgentId: 'agent-support', decision: 'DENY', reason: 'no_permit' }),
      ev(3, { principalAgentId: 'agent-finance', principalRoles: ['finance'] }),
    ]);
    expect(await screen.findByText(/observed identities \(from audit\)/i)).toBeInTheDocument();
    const row = (await screen.findByText('agent-support')).closest('tr')!;
    expect(within(row).getByText('support-agents')).toBeInTheDocument();
    expect(within(row).getByTestId('allowed')).toHaveTextContent('1');
    expect(within(row).getByTestId('denied')).toHaveTextContent('1');
    expect(screen.getByText('agent-finance')).toBeInTheDocument();
  });

  it('shows an empty state', async () => {
    setup([]);
    expect(await screen.findByText(/no identities observed yet/i)).toBeInTheDocument();
  });

  it('shows an error', async () => {
    setup([], vi.fn().mockRejectedValue(new Error('boom')));
    expect(await screen.findByRole('alert')).toHaveTextContent('boom');
  });
});
