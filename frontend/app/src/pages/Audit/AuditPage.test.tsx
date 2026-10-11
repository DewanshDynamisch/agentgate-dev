import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuditPage } from './AuditPage';
import { useGovernanceClient } from '../../state/GovernanceProvider';
import { clientWithWorkflow } from '../../test/test-fixtures';
import { GovernanceApiError } from '@contract/models/governance';
import type { AuditEvent, AuditEventsPage } from '@contract/models/audit';
import type { GovernanceClient } from '@contract/api/governanceClient';

vi.mock('../../state/GovernanceProvider', () => ({
  useGovernanceClient: vi.fn(),
}));

const mockedUseClient = vi.mocked(useGovernanceClient);

function event(seq: number, overrides: Partial<AuditEvent> = {}): AuditEvent {
  return {
    id: `id-${seq}`,
    workspaceId: 'default',
    sequenceNumber: seq,
    executionId: `exec-${seq}`,
    timestamp: '2026-09-29T10:24:01Z',
    eventType: 'tool_call',
    decision: 'ALLOW',
    reason: 'policy_allow',
    principalAgentId: `agent-${seq}`,
    principalRoles: ['support'],
    principalOnBehalfOf: `user-${seq}`,
    toolBackendId: 'crm',
    toolName: `tool-${seq}`,
    toolRisk: 'low',
    policyVersion: 'v1',
    policyHash: 'hash',
    redactedArguments: {},
    ...overrides,
  };
}

function setup(listAuditEvents: GovernanceClient['listAuditEvents']) {
  const client = clientWithWorkflow({ listAuditEvents });
  mockedUseClient.mockReturnValue({ client, workspaceId: 'default' });
  return render(<AuditPage />);
}

const page = (events: AuditEvent[], nextBeforeSequence?: number): AuditEventsPage => ({
  workspaceId: 'default',
  events,
  nextBeforeSequence,
});

describe('AuditPage', () => {
  afterEach(cleanup);

  it('renders rows from listAuditEvents', async () => {
    const list = vi.fn().mockResolvedValue(page([event(2), event(1)]));
    setup(list);

    expect(await screen.findByText('crm/tool-2')).toBeInTheDocument();
    expect(screen.getByText('crm/tool-1')).toBeInTheDocument();
    expect(screen.getByText('agent-2')).toBeInTheDocument();
    expect(screen.getByText('user-2')).toBeInTheDocument();
    expect(screen.getByText('exec-2')).toBeInTheDocument();
    expect(list).toHaveBeenCalledWith('default', expect.objectContaining({ limit: 50 }));
  });

  it('shows the reason on DENY rows', async () => {
    setup(vi.fn().mockResolvedValue(page([event(1, { decision: 'DENY', reason: 'no_matching_policy' })])));
    expect(await screen.findByText('DENY')).toBeInTheDocument();
    expect(screen.getByText('no_matching_policy')).toBeInTheDocument();
  });

  it('loads older events using nextBeforeSequence', async () => {
    const list = vi
      .fn()
      .mockResolvedValueOnce(page([event(3), event(2)], 2))
      .mockResolvedValue(page([event(1)]));
    setup(list);

    fireEvent.click(await screen.findByRole('button', { name: /load older/i }));

    expect(await screen.findByText('crm/tool-1')).toBeInTheDocument();
    expect(list).toHaveBeenLastCalledWith('default', expect.objectContaining({ beforeSequence: 2 }));
    expect(screen.queryByRole('button', { name: /load older/i })).not.toBeInTheDocument();
  });

  it('shows an error banner and no rows when loading fails', async () => {
    setup(vi.fn().mockRejectedValue(new GovernanceApiError(500, 'INTERNAL', 'audit store down')));
    expect(await screen.findByRole('alert')).toHaveTextContent('audit store down');
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('shows an empty state when there are no events', async () => {
    setup(vi.fn().mockResolvedValue(page([])));
    expect(await screen.findByText(/no audit events/i)).toBeInTheDocument();
  });

  it('decision filter hides ALLOW rows', async () => {
    setup(
      vi.fn().mockResolvedValue(
        page([event(2, { decision: 'DENY', reason: 'policy_deny' }), event(1)])
      )
    );
    await screen.findByText('crm/tool-1');

    fireEvent.change(screen.getByLabelText('Decision'), { target: { value: 'DENY' } });

    expect(screen.queryByText('crm/tool-1')).not.toBeInTheDocument();
    expect(screen.getByText('crm/tool-2')).toBeInTheDocument();
  });

  it('tool and agent filters narrow rows client-side', async () => {
    setup(vi.fn().mockResolvedValue(page([event(2), event(1)])));
    await screen.findByText('crm/tool-1');

    fireEvent.change(screen.getByLabelText('Tool'), { target: { value: 'tool-2' } });
    expect(screen.queryByText('crm/tool-1')).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Tool'), { target: { value: '' } });
    fireEvent.change(screen.getByLabelText('Agent'), { target: { value: 'agent-1' } });
    expect(screen.queryByText('crm/tool-2')).not.toBeInTheDocument();
    expect(screen.getByText('crm/tool-1')).toBeInTheDocument();
  });

  it('expands redacted arguments when a row is clicked', async () => {
    setup(vi.fn().mockResolvedValue(page([event(1, { redactedArguments: { customer_id: '[REDACTED]' } })])));
    fireEvent.click(await screen.findByText('crm/tool-1'));
    const args = screen.getByTestId('audit-args-1');
    expect(within(args).getByText('customer_id')).toBeInTheDocument();
    expect(within(args).getByText('[REDACTED]')).toBeInTheDocument();
  });

  describe('auto-refresh', () => {
    beforeEach(() => vi.useFakeTimers());
    afterEach(() => vi.useRealTimers());

    const flush = () => act(async () => { await vi.advanceTimersByTimeAsync(0); });
    const tick = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

    it('refetches the first page every 5s, prepends new events and dedupes by sequenceNumber', async () => {
      const list = vi
        .fn()
        .mockResolvedValueOnce(page([event(2), event(1)]))
        .mockResolvedValue(page([event(3), event(2)]));
      setup(list);
      await flush();
      expect(list).toHaveBeenCalledTimes(1);

      await tick(5000);
      expect(list).toHaveBeenCalledTimes(2);
      expect(screen.getAllByText(/^exec-\d$/).map((n) => n.textContent)).toEqual(['exec-3', 'exec-2', 'exec-1']);
    });

    it('keeps loaded rows visible with a banner when a refresh fails', async () => {
      const list = vi
        .fn()
        .mockResolvedValueOnce(page([event(1)]))
        .mockRejectedValue(new GovernanceApiError(503, 'UNAVAILABLE', 'refresh failed'));
      setup(list);
      await flush();
      await tick(5000);
      expect(screen.getByRole('alert')).toHaveTextContent('refresh failed');
      expect(screen.getByText('crm/tool-1')).toBeInTheDocument();
    });

    it('keeps a load-older error visible after a later successful refresh', async () => {
      const list = vi
        .fn()
        .mockResolvedValueOnce(page([event(3)], 3))
        .mockRejectedValueOnce(new GovernanceApiError(500, 'INTERNAL', 'older failed'))
        .mockResolvedValue(page([event(3)], 3));
      setup(list);
      await flush();
      fireEvent.click(screen.getByRole('button', { name: /load older/i }));
      await flush();
      expect(screen.getByRole('alert')).toHaveTextContent('older failed');
      await tick(5000);
      expect(list).toHaveBeenCalledTimes(3);
      expect(screen.getByRole('alert')).toHaveTextContent('older failed');
      expect(screen.getByText('crm/tool-3')).toBeInTheDocument();
    });

    it('does not poll when auto-refresh is switched off', async () => {
      const list = vi.fn().mockResolvedValue(page([event(1)]));
      setup(list);
      await flush();

      fireEvent.click(screen.getByLabelText(/auto-refresh/i));
      await tick(15000);
      expect(list).toHaveBeenCalledTimes(1);
    });

    it('stops polling on unmount', async () => {
      const list = vi.fn().mockResolvedValue(page([event(1)]));
      const { unmount } = setup(list);
      await flush();
      unmount();
      await tick(15000);
      expect(list).toHaveBeenCalledTimes(1);
    });
  });
});
