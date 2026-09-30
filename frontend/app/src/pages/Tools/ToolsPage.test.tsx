import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ToolsPage } from './ToolsPage';
import { useGovernanceClient } from '../../state/GovernanceProvider';
import { clientWithWorkflow } from '../../test/test-fixtures';
import { GovernanceApiError } from '@contract/models/governance';
import type { ToolDefinition } from '@contract/models/tools';
import type { GovernanceClient } from '@contract/api/governanceClient';

vi.mock('../../state/GovernanceProvider', () => ({
  useGovernanceClient: vi.fn(),
}));

const mockedUseClient = vi.mocked(useGovernanceClient);

function tool(backendId: string, toolName: string, overrides: Partial<ToolDefinition> = {}): ToolDefinition {
  return {
    backendId,
    toolName,
    risk: 'read',
    schema: { type: 'object' },
    arguments: [{ name: 'customer_id', type: 'string', required: true }],
    description: `${toolName} description`,
    updatedAt: '2026-09-29T10:24:01Z',
    updatedBy: 'admin-1',
    ...overrides,
  };
}

const CATALOG = [
  tool('crm', 'get_customer'),
  tool('crm', 'delete_customer', { risk: 'destructive', arguments: [] }),
  tool('billing', 'issue_refund', { risk: 'write', updatedBy: 'admin-2' }),
];

function setup(overrides: Partial<GovernanceClient> = {}) {
  const listTools = vi.fn().mockResolvedValue(CATALOG);
  const upsertTool = vi
    .fn()
    .mockImplementation(async (backendId: string, toolName: string, body: object) => ({
      ...tool(backendId, toolName),
      ...body,
    }));
  const deleteTool = vi.fn().mockResolvedValue(undefined);
  const client = clientWithWorkflow({ listTools, upsertTool, deleteTool, ...overrides });
  mockedUseClient.mockReturnValue({ client, workspaceId: 'default' });
  render(<ToolsPage />);
  return { listTools, upsertTool, deleteTool };
}

async function openEdit(name: string) {
  await screen.findByText(name);
  fireEvent.click(screen.getByRole('button', { name: `Edit ${name}` }));
  return screen.getByRole('dialog');
}

describe('ToolsPage', () => {
  afterEach(cleanup);

  it('renders tools grouped by backend with risk, argument count and updater', async () => {
    setup();
    expect(await screen.findByText('get_customer')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'crm' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'billing' })).toBeInTheDocument();
    const row = screen.getByText('issue_refund').closest('tr')!;
    expect(within(row).getByText('write')).toBeInTheDocument();
    expect(within(row).getByText('admin-2')).toBeInTheDocument();
    expect(within(row).getByText('1')).toBeInTheDocument();
  });

  it('shows an error banner when loading fails', async () => {
    setup({ listTools: vi.fn().mockRejectedValue(new GovernanceApiError(500, 'INTERNAL', 'catalog down')) });
    expect(await screen.findByRole('alert')).toHaveTextContent('catalog down');
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('shows an empty state when the catalog is empty', async () => {
    setup({ listTools: vi.fn().mockResolvedValue([]) });
    expect(await screen.findByText(/no tools registered/i)).toBeInTheDocument();
  });

  it('saves an edited risk through upsertTool and refreshes the list', async () => {
    const { upsertTool, listTools } = setup();
    const dialog = await openEdit('get_customer');
    fireEvent.change(within(dialog).getByLabelText('Risk'), { target: { value: 'write' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(upsertTool).toHaveBeenCalledTimes(1));
    expect(upsertTool).toHaveBeenCalledWith(
      'crm',
      'get_customer',
      expect.objectContaining({
        risk: 'write',
        schema: { type: 'object' },
        arguments: [{ name: 'customer_id', type: 'string', required: true }],
        description: 'get_customer description',
      })
    );
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(listTools).toHaveBeenCalledTimes(2);
  });

  it('keeps tool name read-only on edit', async () => {
    setup();
    const dialog = await openEdit('get_customer');
    expect(within(dialog).getByLabelText('Tool name')).toHaveAttribute('readonly');
    expect(within(dialog).getByLabelText('Backend ID')).toHaveAttribute('readonly');
  });

  it('disables Save when schema is not valid JSON', async () => {
    setup();
    const dialog = await openEdit('get_customer');
    fireEvent.change(within(dialog).getByLabelText('Schema (JSON)'), { target: { value: '{ nope' } });
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  it.each(['null', '[]', '42', '"str"'])('disables Save when schema is not a JSON object (%s)', async (value) => {
    setup();
    const dialog = await openEdit('get_customer');
    fireEvent.change(within(dialog).getByLabelText('Schema (JSON)'), { target: { value } });
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(within(dialog).getByText(/must be a JSON object/i)).toBeInTheDocument();
  });

  it('requires confirmation before saving a change to destructive', async () => {
    const { upsertTool } = setup();
    const dialog = await openEdit('get_customer');
    fireEvent.change(within(dialog).getByLabelText('Risk'), { target: { value: 'destructive' } });
    const save = within(dialog).getByRole('button', { name: 'Save' });
    expect(save).toBeDisabled();

    fireEvent.click(within(dialog).getByLabelText('I understand this changes authorization for all agents'));
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() =>
      expect(upsertTool).toHaveBeenCalledWith('crm', 'get_customer', expect.objectContaining({ risk: 'destructive' }))
    );
  });

  it('does not demand confirmation when a destructive tool stays destructive', async () => {
    setup();
    const dialog = await openEdit('delete_customer');
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeEnabled();
  });

  it('shows the server message inline on a 400 and keeps the dialog open', async () => {
    const upsertTool = vi
      .fn()
      .mockRejectedValue(new GovernanceApiError(400, 'INVALID_ARGUMENT', 'schema does not declare argument customer_id'));
    setup({ upsertTool });
    const dialog = await openEdit('get_customer');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('schema does not declare argument customer_id');
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeEnabled();
  });

  it('registers a new tool with edited arguments', async () => {
    const { upsertTool } = setup();
    await screen.findByText('get_customer');
    fireEvent.click(screen.getByRole('button', { name: 'Register tool' }));
    const dialog = screen.getByRole('dialog');
    const save = within(dialog).getByRole('button', { name: 'Save' });
    expect(save).toBeDisabled();

    fireEvent.change(within(dialog).getByLabelText('Backend ID'), { target: { value: 'hr' } });
    fireEvent.change(within(dialog).getByLabelText('Tool name'), { target: { value: 'get_employee' } });
    fireEvent.change(within(dialog).getByLabelText('Description'), { target: { value: 'fetch one' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add argument' }));
    fireEvent.change(within(dialog).getByLabelText('Argument 1 name'), { target: { value: 'id' } });
    fireEvent.change(within(dialog).getByLabelText('Argument 1 type'), { target: { value: 'int64' } });
    fireEvent.click(within(dialog).getByLabelText('Argument 1 required'));
    expect(save).toBeEnabled();
    fireEvent.click(save);

    await waitFor(() =>
      expect(upsertTool).toHaveBeenCalledWith(
        'hr',
        'get_employee',
        expect.objectContaining({
          risk: 'read',
          description: 'fetch one',
          arguments: [{ name: 'id', type: 'int64', required: true }],
        })
      )
    );
  });

  it('enforces typing the tool name before delete', async () => {
    const { deleteTool, listTools } = setup();
    await screen.findByText('get_customer');
    fireEvent.click(screen.getByRole('button', { name: 'Delete get_customer' }));
    const dialog = screen.getByRole('dialog');
    const confirm = within(dialog).getByRole('button', { name: 'Delete tool' });
    expect(confirm).toBeDisabled();

    fireEvent.change(within(dialog).getByLabelText(/type the tool name/i), { target: { value: 'get_custome' } });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText(/type the tool name/i), { target: { value: 'get_customer' } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);

    await waitFor(() => expect(deleteTool).toHaveBeenCalledWith('crm', 'get_customer'));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(listTools).toHaveBeenCalledTimes(2);
  });

  it('shows a delete failure inline', async () => {
    setup({ deleteTool: vi.fn().mockRejectedValue(new GovernanceApiError(404, 'NOT_FOUND', 'tool not found')) });
    await screen.findByText('get_customer');
    fireEvent.click(screen.getByRole('button', { name: 'Delete get_customer' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText(/type the tool name/i), { target: { value: 'get_customer' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete tool' }));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('tool not found');
  });

  async function openRegister() {
    await screen.findByText('get_customer');
    fireEvent.click(screen.getByRole('button', { name: 'Register tool' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Backend ID'), { target: { value: 'hr' } });
    fireEvent.change(within(dialog).getByLabelText('Tool name'), { target: { value: 'get_employee' } });
    return dialog;
  }

  it.each([['space', 'bad name'], ['slash', 'a/b'], ['129 chars', 'a'.repeat(129)]])(
    'disables Save for an invalid tool name (%s)',
    async (_l, value) => {
      setup();
      const dialog = await openRegister();
      expect(within(dialog).getByRole('button', { name: 'Save' })).toBeEnabled();
      fireEvent.change(within(dialog).getByLabelText('Tool name'), { target: { value } });
      expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled();
      expect(within(dialog).getByText(/tool name may only contain/i)).toBeInTheDocument();
    }
  );

  it('disables Save for an invalid backend id', async () => {
    setup();
    const dialog = await openRegister();
    fireEvent.change(within(dialog).getByLabelText('Backend ID'), { target: { value: 'my backend' } });
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(within(dialog).getByText(/backend id may only contain/i)).toBeInTheDocument();
  });

  it('disables Save for empty and duplicate argument names, including trailing-space variants', async () => {
    setup();
    const dialog = await openRegister();
    const save = within(dialog).getByRole('button', { name: 'Save' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add argument' }));
    expect(save).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText('Argument 1 name'), { target: { value: 'id' } });
    expect(save).toBeEnabled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add argument' }));
    fireEvent.change(within(dialog).getByLabelText('Argument 2 name'), { target: { value: 'id ' } });
    expect(save).toBeDisabled();
    expect(within(dialog).getByText(/duplicate argument name/i)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Argument 2 name'), { target: { value: 'other' } });
    expect(save).toBeEnabled();
  });

  it('resets the destructive confirmation when risk moves away and when reopened', async () => {
    setup();
    let dialog = await openEdit('get_customer');
    fireEvent.change(within(dialog).getByLabelText('Risk'), { target: { value: 'destructive' } });
    const label = 'I understand this changes authorization for all agents';
    fireEvent.click(within(dialog).getByLabelText(label));
    fireEvent.change(within(dialog).getByLabelText('Risk'), { target: { value: 'write' } });
    fireEvent.change(within(dialog).getByLabelText('Risk'), { target: { value: 'destructive' } });
    expect(within(dialog).getByLabelText(label)).not.toBeChecked();
    fireEvent.click(within(dialog).getByLabelText(label));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    dialog = await openEdit('get_customer');
    fireEvent.change(within(dialog).getByLabelText('Risk'), { target: { value: 'destructive' } });
    expect(within(dialog).getByLabelText(label)).not.toBeChecked();
  });
});
