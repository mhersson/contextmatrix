import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { JiraImportDialog } from './JiraImportDialog';
import { api } from '../../api/client';
import type { BoardEvent, JiraImportState } from '../../types';
import { samplePreview } from './previewFixture';

vi.mock('../../api/client', () => ({
  api: {
    previewJiraEpic: vi.fn(),
    importJiraEpic: vi.fn(),
    getJiraImport: vi.fn(),
  },
  isAPIError: (err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
}));

vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({
    projects: [
      { name: 'api', display_name: 'API', prefix: 'API' },
      { name: 'web', display_name: 'Web', prefix: 'WEB' },
      { name: 'legacy', display_name: 'Legacy', prefix: 'PROJ' },
    ],
    loading: false,
    error: null,
    connected: true,
    refreshProjects: () => {},
  }),
}));

// A stand-in SSE bus: one stable subscribe, handlers by exact type, and a
// reconnect epoch the test bumps.
const sse = vi.hoisted(() => {
  const handlers = new Map<string, (e: BoardEvent) => void>();
  return {
    handlers,
    reconnectEpoch: 0,
    subscribe: (pattern: string, fn: (e: BoardEvent) => void) => {
      handlers.set(pattern, fn);
      return () => {
        if (handlers.get(pattern) === fn) handlers.delete(pattern);
      };
    },
  };
});

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: sse.subscribe, connected: true, error: null, reconnectEpoch: sse.reconnectEpoch }),
}));

function emit(type: string, data: Record<string, unknown>) {
  act(() => {
    sse.handlers.get(type)?.({ type, project: '', card_id: '', timestamp: '', data } as BoardEvent);
  });
}

function state(status: JiraImportState['status'], extra: Partial<JiraImportState> = {}): JiraImportState {
  return { id: 'ji-1', epic: 'PROJ-1', status, total: 3, results: [], started_at: '2026-10-12T09:00:00Z', ...extra };
}

function tree(description = '', onClose = vi.fn()) {
  return (
    <MemoryRouter initialEntries={['/playbooks']}>
      <Routes>
        <Route path="playbooks" element={<JiraImportDialog epicKey="PROJ-1" description={description} onClose={onClose} />} />
        <Route path="playbooks/:id" element={<p>playbook page</p>} />
      </Routes>
    </MemoryRouter>
  );
}

const importButton = () => screen.getByRole('button', { name: /^Import 3 issues/ });
const resultRows = () => screen.getByRole('list', { name: 'Import results' }).querySelectorAll('li');

async function startImport() {
  fireEvent.change(await screen.findByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
  // A real click focuses the button, which the results view then removes.
  importButton().focus();
  fireEvent.click(importButton());
  await waitFor(() => expect(api.getJiraImport).toHaveBeenCalledWith('ji-1'));
}

describe('JiraImportDialog', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    sse.handlers.clear();
    sse.reconnectEpoch = 0;
    vi.mocked(api.previewJiraEpic).mockResolvedValue(samplePreview());
    vi.mocked(api.importJiraEpic).mockResolvedValue({ import_id: 'ji-1' });
    vi.mocked(api.getJiraImport).mockResolvedValue(state('running'));
  });

  it('opens as a dialog named for the epic', async () => {
    render(tree());
    expect(screen.getByRole('dialog', { name: /Import Jira epic PROJ-1/ })).toHaveFocus();
    expect(await screen.findByRole('dialog', { name: /Checkout revamp/ })).toBeInTheDocument();
    expect(api.previewJiraEpic).toHaveBeenCalledWith('PROJ-1');
  });

  it('defaults the selection and disables issues already in the playbook', async () => {
    render(tree());
    expect(await screen.findByLabelText('Import PROJ-2')).toBeChecked();
    expect(screen.getByLabelText('Import PROJ-3')).toBeChecked();
    expect(screen.getByLabelText('Import PROJ-4')).not.toBeChecked();
    expect(screen.getByLabelText('Import PROJ-5')).toBeDisabled();
    expect(screen.getByLabelText('Import PROJ-6')).toBeEnabled();
    expect(screen.getByLabelText('Mark imported cards vetted')).not.toBeChecked();
    expect(screen.getByLabelText('Include epic description in each card')).toBeChecked();
  });

  it('sets every non-imported issue from the all-issues picker', async () => {
    render(tree());
    fireEvent.change(await screen.findByLabelText('Project for all issues'), { target: { value: 'web' } });
    expect(screen.getByLabelText('Project for PROJ-2')).toHaveValue('web');
    expect(screen.getByLabelText('Project for PROJ-7')).toHaveValue('web');
    expect(screen.getByLabelText('Project for PROJ-5')).toHaveValue('api');
  });

  it('warns when a sub-task leaves its parent project', async () => {
    render(tree());
    await screen.findByLabelText('Import PROJ-3');
    expect(screen.queryAllByRole('note', { name: /split from/ })).toHaveLength(0);
    fireEvent.change(screen.getByLabelText('Project for PROJ-3'), { target: { value: 'web' } });
    expect(screen.getAllByRole('note', { name: /split from/ })).toHaveLength(1);
  });

  it('warns, without blocking, when a card prefix equals a Jira project key', async () => {
    render(tree());
    fireEvent.change(await screen.findByLabelText('Project for PROJ-7'), { target: { value: 'legacy' } });
    expect(screen.getByRole('note', { name: 'PROJ-7 prefix clash' })).toBeInTheDocument();
    expect(importButton()).toBeEnabled();

    fireEvent.change(screen.getByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
    expect(screen.queryByRole('note', { name: 'PROJ-7 prefix clash' })).not.toBeInTheDocument();
  });

  it('shows the existing playbook title read-only', async () => {
    vi.mocked(api.previewJiraEpic).mockResolvedValue(samplePreview({ playbook: { id: 'p', title: 'Existing' } }));
    render(tree());
    const title = await screen.findByLabelText('Playbook title');
    expect(title).toHaveValue('Existing');
    expect(title).toHaveAttribute('readonly');
  });

  it('says up front that the playbook has an active run and keeps Import disabled', async () => {
    vi.mocked(api.previewJiraEpic).mockResolvedValue(
      samplePreview({ playbook: { id: 'p', title: 'Existing', run_active: true } }),
    );
    render(tree());
    expect(await screen.findByRole('alert', { name: /^Playbook run in progress/ })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
    expect(importButton()).toBeDisabled();
  });

  it('gives the same guidance when a run starts between the preview and the import', async () => {
    vi.mocked(api.importJiraEpic).mockRejectedValue({ error: 'playbook run is active', code: 'JIRA_PLAYBOOK_RUNNING' });
    render(tree());
    fireEvent.change(await screen.findByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    importButton().focus();
    fireEvent.click(importButton());

    expect(await screen.findByRole('alert', { name: /^Playbook run in progress/ })).toBeInTheDocument();
    expect(importButton()).toBeDisabled();
    expect(screen.getByRole('dialog')).toHaveFocus();
    expect(screen.queryByRole('list', { name: 'Import results' })).not.toBeInTheDocument();
  });

  it('shows the server message when the import is refused and allows a retry', async () => {
    vi.mocked(api.importJiraEpic).mockRejectedValueOnce({ error: 'imports are paused', code: 'JIRA_IMPORT_UNAVAILABLE' });
    render(tree());
    fireEvent.change(await screen.findByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
    importButton().focus();
    fireEvent.click(importButton());

    expect(await screen.findByRole('alert')).toHaveTextContent('imports are paused');
    expect(screen.getByRole('dialog')).toHaveFocus();
    expect(importButton()).toBeEnabled();
    fireEvent.click(importButton());
    await waitFor(() => expect(api.getJiraImport).toHaveBeenCalledWith('ji-1'));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('starts the import, follows its events and opens the playbook', async () => {
    render(tree('Typed'));
    fireEvent.click(await screen.findByLabelText('Mark imported cards vetted'));
    await startImport();

    expect(screen.getByRole('dialog')).toHaveFocus();
    expect(api.importJiraEpic).toHaveBeenCalledWith(
      'PROJ-1',
      expect.objectContaining({
        vetted: true,
        include_epic_context: true,
        playbook: { title: 'PROJ-1 Checkout revamp', description: 'Typed' },
      }),
    );
    expect(screen.getByRole('button', { name: 'Open playbook' })).toBeDisabled();

    emit('jira_import.progress', { import_id: 'ji-other', key: 'PROJ-9', status: 'created', project: 'api', card_id: 'API-009', reason: '' });
    emit('jira_import.progress', { import_id: 'ji-1', key: 'PROJ-3', status: 'created', project: 'api', card_id: 'API-001', reason: '' });
    expect(resultRows()).toHaveLength(1);
    expect(resultRows()[0]).toHaveTextContent('API-001');

    emit('jira_import.done', {
      import_id: 'ji-1',
      status: 'done',
      playbook_id: 'proj-1-checkout-revamp',
      error: '',
      results: [
        { key: 'PROJ-3', status: 'created', project: 'api', card: 'API-001' },
        { key: 'PROJ-2', status: 'created', project: 'api', card: 'API-002' },
        { key: 'PROJ-7', status: 'failed', project: 'web', reason: 'project not found' },
      ],
    });
    expect(resultRows()).toHaveLength(3);

    fireEvent.click(screen.getByRole('button', { name: 'Open playbook' }));
    expect(await screen.findByText('playbook page')).toBeInTheDocument();
  });

  it('lists the selections a stopped import never reached as not run', async () => {
    render(tree());
    await startImport();

    emit('jira_import.done', {
      import_id: 'ji-1',
      status: 'failed',
      playbook_id: 'proj-1-checkout-revamp',
      error: 'the import was interrupted',
      results: [{ key: 'PROJ-2', status: 'created', project: 'api', card: 'API-002' }],
    });

    const rows = resultRows();
    expect(rows).toHaveLength(3);
    const notRun = Array.from(rows).filter((li) => within(li).queryByText('not run'));
    expect(notRun.map((li) => li.textContent)).toEqual([
      expect.stringContaining('PROJ-3'),
      expect.stringContaining('PROJ-7'),
    ]);
    expect(screen.getByRole('status')).toHaveTextContent('the import was interrupted');
  });

  it('catches up from the server after an SSE reconnect', async () => {
    const view = render(tree());
    await startImport();
    expect(screen.getByRole('button', { name: 'Open playbook' })).toBeDisabled();

    // The done event was lost while the stream was down.
    vi.mocked(api.getJiraImport).mockResolvedValue(
      state('done', {
        playbook_id: 'proj-1-checkout-revamp',
        results: [{ key: 'PROJ-2', status: 'created', project: 'api', card: 'API-002' }],
      }),
    );
    sse.reconnectEpoch = 1;
    view.rerender(tree());

    await waitFor(() => expect(screen.getByRole('button', { name: 'Open playbook' })).toBeEnabled());
    expect(resultRows()).toHaveLength(1);
  });

  it('keeps the progress the events built when a catch-up knows less', async () => {
    let resolveCatchUp: (s: JiraImportState) => void = () => {};
    vi.mocked(api.getJiraImport).mockReturnValueOnce(new Promise((resolve) => { resolveCatchUp = resolve; }));
    render(tree());
    await startImport();

    emit('jira_import.progress', { import_id: 'ji-1', key: 'PROJ-2', status: 'created', project: 'api', card_id: 'API-002', reason: '' });
    await act(async () => resolveCatchUp(state('running')));
    expect(resultRows()).toHaveLength(1);
  });

  it('keeps the outcome when a catch-up answer lands after the done event', async () => {
    let resolveCatchUp: (s: JiraImportState) => void = () => {};
    vi.mocked(api.getJiraImport).mockReturnValueOnce(new Promise((resolve) => { resolveCatchUp = resolve; }));
    render(tree());
    await startImport();

    emit('jira_import.done', { import_id: 'ji-1', status: 'failed', playbook_id: '', error: 'stopped', results: [] });
    await act(async () => resolveCatchUp(state('running')));
    expect(resultRows()).toHaveLength(3);
    expect(screen.getByRole('status')).toHaveTextContent('stopped');
  });

  it('merges a catch-up by key, so a row only the events saw is kept', async () => {
    let resolveCatchUp: (s: JiraImportState) => void = () => {};
    vi.mocked(api.getJiraImport).mockReturnValueOnce(new Promise((resolve) => { resolveCatchUp = resolve; }));
    render(tree());
    await startImport();

    // The PROJ-3 event was dropped; the snapshot predates PROJ-7.
    emit('jira_import.progress', { import_id: 'ji-1', key: 'PROJ-2', status: 'created', project: 'api', card_id: 'API-002', reason: '' });
    emit('jira_import.progress', { import_id: 'ji-1', key: 'PROJ-7', status: 'created', project: 'web', card_id: 'WEB-001', reason: '' });
    await act(async () =>
      resolveCatchUp(
        state('running', {
          results: [
            { key: 'PROJ-2', status: 'created', project: 'api', card: 'API-002' },
            { key: 'PROJ-3', status: 'created', project: 'api', card: 'API-003' },
          ],
        }),
      ),
    );
    expect(Array.from(resultRows()).map((li) => li.textContent)).toEqual([
      expect.stringContaining('PROJ-2'),
      expect.stringContaining('PROJ-3'),
      expect.stringContaining('PROJ-7'),
    ]);
  });

  it('ends an import the server has forgotten and lists its selections as not run', async () => {
    vi.mocked(api.getJiraImport).mockRejectedValue({ error: 'import not found', code: 'JIRA_IMPORT_NOT_FOUND' });
    render(tree());
    await startImport();

    await waitFor(() => expect(resultRows()).toHaveLength(3));
    expect(within(screen.getByRole('list', { name: 'Import results' })).getAllByText('not run')).toHaveLength(3);
    expect(screen.getByRole('button', { name: 'Open playbook' })).toBeDisabled();
  });

  it('keeps following the import when a catch-up fails for another reason', async () => {
    vi.mocked(api.getJiraImport).mockRejectedValue(new TypeError('Failed to fetch'));
    render(tree());
    await startImport();
    await act(async () => {});

    expect(resultRows()).toHaveLength(0);
    emit('jira_import.progress', { import_id: 'ji-1', key: 'PROJ-2', status: 'created', project: 'api', card_id: 'API-002', reason: '' });
    expect(resultRows()).toHaveLength(1);
  });

  describe('catch-up timer', () => {
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    const tick = () => act(() => vi.advanceTimersByTimeAsync(5_000));

    // waitFor polls on setInterval, which is frozen here; act flushes the POST and the effects instead.
    async function startImportNow() {
      fireEvent.change(await screen.findByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
      await act(async () => fireEvent.click(importButton()));
      await act(async () => {});
    }

    it('polls every 5 s while running and stops once the import is done', async () => {
      render(tree());
      await startImportNow();
      expect(api.getJiraImport).toHaveBeenCalledTimes(1);
      await tick();
      await tick();
      expect(api.getJiraImport).toHaveBeenCalledTimes(3);

      emit('jira_import.done', { import_id: 'ji-1', status: 'done', playbook_id: 'p', error: '', results: [] });
      await tick();
      await tick();
      expect(api.getJiraImport).toHaveBeenCalledTimes(3);
    });

    it('stops polling when the dialog unmounts', async () => {
      const view = render(tree());
      await startImportNow();
      expect(api.getJiraImport).toHaveBeenCalledTimes(1);
      view.unmount();
      await tick();
      await tick();
      expect(api.getJiraImport).toHaveBeenCalledTimes(1);
    });
  });

  it('rejoins the epic’s running import instead of offering Import', async () => {
    vi.mocked(api.previewJiraEpic).mockResolvedValue(
      samplePreview({ active_import: { id: 'ji-1', keys: ['PROJ-3', 'PROJ-2', 'PROJ-7'] } }),
    );
    render(tree());

    await waitFor(() => expect(api.getJiraImport).toHaveBeenCalledWith('ji-1'));
    expect(screen.queryByRole('button', { name: /^Import/ })).not.toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent('Importing… 0 of 3');

    emit('jira_import.done', {
      import_id: 'ji-1',
      status: 'failed',
      playbook_id: 'proj-1-checkout-revamp',
      error: 'timed out; run the import again',
      results: [{ key: 'PROJ-3', status: 'created', project: 'api', card: 'API-001' }],
    });
    expect(within(screen.getByRole('list', { name: 'Import results' })).getAllByText('not run')).toHaveLength(2);
    expect(api.importJiraEpic).not.toHaveBeenCalled();
  });

  it('shows the server error when the preview fails', async () => {
    vi.mocked(api.previewJiraEpic).mockRejectedValue({ error: 'Jira authentication failed', code: 'JIRA_AUTH_FAILED' });
    render(tree());
    expect(await screen.findByRole('alert')).toHaveTextContent('Jira authentication failed');
    expect(screen.getByRole('dialog', { name: /Import Jira epic PROJ-1/ })).toBeInTheDocument();
  });

  it('shows the details of a refused import', async () => {
    vi.mocked(api.importJiraEpic).mockRejectedValueOnce({
      error: 'invalid import request',
      code: 'BAD_REQUEST',
      details: 'at most 200 issues per import',
    });
    render(tree());
    fireEvent.change(await screen.findByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
    fireEvent.click(importButton());
    expect(await screen.findByRole('alert')).toHaveTextContent('at most 200 issues per import');
  });

  it('shows both the error and the details when the preview is rejected', async () => {
    vi.mocked(api.previewJiraEpic).mockRejectedValue({
      error: 'Jira refused the request',
      code: 'JIRA_REQUEST_REJECTED',
      details: 'Field x is invalid',
    });
    render(tree());
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Jira refused the request');
    expect(alert).toHaveTextContent('Field x is invalid');
  });

  it('disables Import above the selection cap', async () => {
    const base = samplePreview();
    const children = Array.from({ length: 201 }, (_, i) => ({ ...base.children[0], key: `PROJ-${100 + i}`, subtasks: [] }));
    vi.mocked(api.previewJiraEpic).mockResolvedValue(samplePreview({ children }));
    render(tree());
    expect(await screen.findByRole('button', { name: /^Import 201 issues/ })).toBeDisabled();
  });

  it('closes on Escape', async () => {
    const onClose = vi.fn();
    render(tree('', onClose));
    await screen.findByLabelText('Import PROJ-2');
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('can be closed while the preview loads', () => {
    vi.mocked(api.previewJiraEpic).mockReturnValue(new Promise(() => {}));
    const onClose = vi.fn();
    render(tree('', onClose));
    fireEvent.click(screen.getByRole('button', { name: /^(Close|Cancel)$/ }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('can be closed after the preview fails', async () => {
    vi.mocked(api.previewJiraEpic).mockRejectedValue({ error: 'Jira authentication failed', code: 'JIRA_AUTH_FAILED' });
    const onClose = vi.fn();
    render(tree('', onClose));
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: /^(Close|Cancel)$/ }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
