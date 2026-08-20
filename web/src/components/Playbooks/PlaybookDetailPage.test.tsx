import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { PlaybookDetailPage } from './PlaybookDetailPage';
import { persistReorder } from './playbookUtils';
import { api } from '../../api/client';
import type { PlaybookDetail } from '../../types';

vi.mock('../../api/client', () => ({
  api: {
    getPlaybook: vi.fn(),
    patchPlaybook: vi.fn(),
    patchPlaybookEntry: vi.fn(),
    deletePlaybookEntry: vi.fn(),
    addPlaybookEntry: vi.fn(),
    deletePlaybook: vi.fn(),
    getCards: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, error: null, reconnectEpoch: 0 }),
}));

vi.mock('../../hooks/useToast', () => ({
  useToast: () => ({ showToast: () => {} }),
}));

vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({
    projects: [{ name: 'alpha', display_name: 'Alpha' }],
    loading: false, error: null, connected: true, refreshProjects: () => {},
  }),
}));

vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: false, toggle: () => {}, close: () => {} }),
}));

function baseDetail(): PlaybookDetail {
  return {
    id: 'roll', title: 'Roll', created_at: '2026-08-20T09:00:00Z', updated_at: '2026-08-20T09:00:00Z',
    complete: 1, total: 2,
    entries: [
      { id: 'e1', type: 'manual', text: 'done step', done: true, complete: true },
      { id: 'e2', type: 'card', project: 'alpha', card: 'ALPHA-101', card_title: 'The card', card_state: 'todo', complete: false },
    ],
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/playbooks/roll']}>
      <Routes>
        <Route path="playbooks/:id" element={<PlaybookDetailPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('PlaybookDetailPage', () => {
  beforeEach(() => {
    vi.mocked(api.getPlaybook).mockReset();
    vi.mocked(api.patchPlaybookEntry).mockReset();
  });

  it('renders header progress and both entries from the fetch', async () => {
    vi.mocked(api.getPlaybook).mockResolvedValue(baseDetail());
    renderPage();
    expect(await screen.findByText('Roll')).toBeInTheDocument();
    expect(screen.getByText('The card')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /1 of 2 complete/i })).toBeInTheDocument();
  });

  it('links the breadcrumb back to the playbooks list', async () => {
    vi.mocked(api.getPlaybook).mockResolvedValue(baseDetail());
    renderPage();
    await screen.findByText('Roll');
    const crumb = screen.getByRole('link', { name: /^playbooks$/i });
    expect(crumb.getAttribute('href')).toBe('/playbooks');
  });

  it('shows the not-found state when the fetch rejects', async () => {
    vi.mocked(api.getPlaybook).mockRejectedValueOnce({ error: 'playbook not found', code: 'PLAYBOOK_NOT_FOUND' });
    renderPage();
    expect(await screen.findByText(/not found/i)).toBeInTheDocument();
  });

  it('persists a reorder with the final index', async () => {
    vi.mocked(api.patchPlaybookEntry).mockResolvedValue(baseDetail());
    await persistReorder('roll', baseDetail(), 'e2', 'e1');
    expect(api.patchPlaybookEntry).toHaveBeenCalledWith('roll', 'e2', { position: 0 });
  });

  it('does not reset the add composer until the add request actually settles', async () => {
    vi.mocked(api.getPlaybook).mockResolvedValue(baseDetail());
    let resolveAdd: (value: PlaybookDetail) => void = () => {};
    vi.mocked(api.addPlaybookEntry).mockReturnValue(new Promise((resolve) => { resolveAdd = resolve; }));
    renderPage();
    await screen.findByText('Roll');

    fireEvent.click(screen.getByRole('button', { name: /^manual$/i }));
    const textInput = screen.getByPlaceholderText('Manual step');
    fireEvent.change(textInput, { target: { value: 'ship it' } });
    const addButton = screen.getByRole('button', { name: /add entry/i });
    fireEvent.click(addButton);
    expect(addButton).toBeDisabled();

    // Give any pending microtasks a chance to run. Before the fix,
    // handleAdd's await resolved without waiting on the real API call, so
    // the composer would already have reset here - clearing the button's
    // disabled guard and the typed text before the request settled.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(addButton).toBeDisabled();
    expect(textInput).toHaveValue('ship it');

    resolveAdd(baseDetail());
    await waitFor(() => expect(textInput).toHaveValue(''));
    expect(api.addPlaybookEntry).toHaveBeenCalledTimes(1);
  });
});
