import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { SessionsPage } from './SessionsPage';
import { api } from '../../api/client';
import type { ChatSession } from '../../types';

const toast = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('../../hooks/useToast', () => ({ useToast: () => toast }));

vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: false, toggle: () => {}, close: () => {} }),
}));

function sess(o: Partial<ChatSession> & { id: string }): ChatSession {
  return {
    title: o.id,
    status: 'cold',
    created_at: '2026-10-01T10:00:00Z',
    last_active: '2026-10-01T10:00:00Z',
    created_by: 'human:alice',
    ...o,
  };
}

const TITLES = ['Fix login', 'Shopping list', 'Recipe box', 'Uptime endpoint'];

let store: ChatSession[] = [];

beforeEach(() => {
  toast.showToast.mockReset();
  store = [
    sess({ id: 'c1', title: 'Fix login', project: 'api', estimated_cost_usd: 0.5, last_active: '2026-10-20T10:00:00Z' }),
    sess({
      id: 'b1',
      title: 'Shopping list',
      kind: 'architect',
      mode: 'yolo',
      workspace: ['shop'],
      spent_usd: 1.42,
      estimated_cost_usd: 9.99,
      last_active: '2026-10-22T10:00:00Z',
    }),
    sess({
      id: 'b2',
      title: 'Recipe box',
      kind: 'architect',
      mode: 'default',
      workspace: ['recipes'],
      estimated_cost_usd: 7.77,
      last_active: '2026-10-23T10:00:00Z',
    }),
    sess({
      id: 'b3',
      title: 'Uptime endpoint',
      kind: 'architect',
      mode: 'auto',
      workspace: ['homelander'],
      spent_usd: 0.38,
      last_active: '2026-10-21T10:00:00Z',
    }),
  ];
  vi.spyOn(api, 'listChats').mockImplementation(async () => store.map((s) => ({ ...s })));
  vi.spyOn(api, 'updateChat').mockImplementation(async (id, patch) => {
    store = store.map((s) => (s.id === id ? { ...s, pinned: patch.pinned } : s));
    return store.find((s) => s.id === id)!;
  });
  vi.spyOn(api, 'deleteChat').mockImplementation(async (id) => {
    store = store.filter((s) => s.id !== id);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

function renderAt(url = '/sessions?tab=blueprints') {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/sessions" element={<SessionsPage />} />
        <Route path="*" element={null} />
      </Routes>
      <Where />
    </MemoryRouter>,
  );
}

const where = () => screen.getByTestId('where').textContent;
const row = (title: string) => screen.getByText(title).closest('tr') as HTMLElement;
const listedTitles = () =>
  screen
    .queryAllByRole('row')
    .map((r) => TITLES.find((t) => within(r).queryByText(t)))
    .filter(Boolean);
const tick = (title: string) => fireEvent.click(within(row(title)).getByRole('checkbox'));
const bulkBar = () => screen.queryByRole('toolbar');

describe('SessionsPage', () => {
  it('opens on the tab in the URL and shows both counts', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    const blueprints = screen.getByRole('tab', { name: /blueprints/i });
    const chats = screen.getByRole('tab', { name: /chats/i });
    expect(blueprints).toHaveAttribute('aria-selected', 'true');
    expect(chats).toHaveAttribute('aria-selected', 'false');
    expect(within(blueprints).getByText('3')).toBeInTheDocument();
    expect(within(chats).getByText('1')).toBeInTheDocument();
    expect(listedTitles()).toHaveLength(3);
    expect(screen.queryByText('Fix login')).toBeNull();
  });

  it('an unknown tab opens the chats tab', async () => {
    renderAt('/sessions?tab=bogus');
    await screen.findByText('Fix login');

    expect(screen.getByRole('tab', { name: /chats/i })).toHaveAttribute('aria-selected', 'true');
    expect(listedTitles()).toEqual(['Fix login']);
  });

  it('switching tabs writes the tab to the URL', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    fireEvent.click(screen.getByRole('tab', { name: /chats/i }));

    expect(where()).toBe('/sessions?tab=chats');
    expect(listedTitles()).toEqual(['Fix login']);
  });

  it('rows are newest activity first and clicking one opens its pane', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    expect(listedTitles()).toEqual(['Recipe box', 'Shopping list', 'Uptime endpoint']);

    fireEvent.click(screen.getByText('Shopping list'));
    expect(where()).toBe('/chat/b1');
  });

  it('a blueprint row shows its recorded spend and its mode; one without spend shows no amount', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    const spent = row('Shopping list');
    expect(within(spent).getByText('$1.42')).toBeInTheDocument();
    expect(within(spent).queryByText('$9.99')).toBeNull();
    expect(within(spent).getByText('yolo')).toBeInTheDocument();

    const unrecorded = row('Recipe box');
    expect(within(unrecorded).queryByText(/\$/)).toBeNull();
    expect(within(unrecorded).getByText('default')).toBeInTheDocument();
  });

  it('the pin toggle patches the session without opening it', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    fireEvent.click(within(row('Recipe box')).getByRole('button', { pressed: false }));

    await waitFor(() => expect(api.updateChat).toHaveBeenCalledWith('b2', { pinned: true }));
    expect(api.updateChat).toHaveBeenCalledTimes(1);
    expect(where()).toBe('/sessions?tab=blueprints');
    await waitFor(() => expect(within(row('Recipe box')).getByRole('button', { pressed: true })).toBeInTheDocument());
  });

  it('bulk delete asks first, naming the count, then deletes each ticked session', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    tick('Recipe box');
    tick('Uptime endpoint');
    fireEvent.click(within(bulkBar()!).getByRole('button', { name: /delete/i }));

    const dialog = screen.getByRole('dialog');
    expect(dialog.textContent).toContain('2');
    expect(api.deleteChat).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /delete/i }));

    await waitFor(() => expect(api.deleteChat).toHaveBeenCalledTimes(2));
    expect(api.deleteChat).toHaveBeenCalledWith('b2');
    expect(api.deleteChat).toHaveBeenCalledWith('b3');
    await waitFor(() => expect(listedTitles()).toEqual(['Shopping list']));
    expect(bulkBar()).toBeNull();
  });

  it('bulk delete reports failures and keeps them ticked', async () => {
    vi.mocked(api.deleteChat).mockImplementation(async (id) => {
      if (id === 'b3') throw new Error('not found');
      store = store.filter((s) => s.id !== id);
    });
    renderAt();
    await screen.findByText('Recipe box');

    tick('Recipe box');
    tick('Uptime endpoint');
    fireEvent.click(within(bulkBar()!).getByRole('button', { name: /delete/i }));
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /delete/i }));

    await waitFor(() => expect(screen.queryByText('Recipe box')).toBeNull());
    expect(api.listChats).toHaveBeenCalledTimes(2);
    expect(within(row('Uptime endpoint')).getByRole('checkbox')).toBeChecked();
    expect(within(row('Shopping list')).getByRole('checkbox')).not.toBeChecked();
    expect(within(bulkBar()!).getByText('1')).toBeInTheDocument();
    expect(toast.showToast).toHaveBeenCalledTimes(1);
    expect(toast.showToast.mock.calls[0][1]).toBe('error');
  });

  it('bulk actions apply only to visible ticked rows', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    tick('Recipe box');
    tick('Shopping list');
    expect(within(bulkBar()!).getByText('2')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'recipe' } });
    expect(within(bulkBar()!).getByText('1')).toBeInTheDocument();

    fireEvent.click(within(bulkBar()!).getByRole('button', { name: 'Pin' }));
    await waitFor(() => expect(api.updateChat).toHaveBeenCalledTimes(1));
    expect(api.updateChat).toHaveBeenCalledWith('b2', { pinned: true });

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } });
    expect(within(row('Shopping list')).getByRole('checkbox')).not.toBeChecked();

    fireEvent.click(screen.getByRole('tab', { name: /chats/i }));
    fireEvent.click(screen.getByRole('tab', { name: /blueprints/i }));
    expect(bulkBar()).toBeNull();
    expect(screen.getAllByRole('checkbox').filter((c) => (c as HTMLInputElement).checked)).toHaveLength(0);
  });

  it('the header tick box selects every visible row', async () => {
    renderAt();
    await screen.findByText('Recipe box');

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'list' } });
    fireEvent.click(screen.getByRole('checkbox', { name: /all/i }));
    fireEvent.click(within(bulkBar()!).getByRole('button', { name: 'Unpin' }));

    await waitFor(() => expect(api.updateChat).toHaveBeenCalledTimes(1));
    expect(api.updateChat).toHaveBeenCalledWith('b1', { pinned: false });
  });

  it('a failed load shows an alert', async () => {
    vi.mocked(api.listChats).mockRejectedValueOnce(new Error('boom'));
    renderAt();

    expect(await screen.findByRole('alert')).toBeInTheDocument();
  });

  it('an empty tab and a filter with no matches each show an empty state', async () => {
    store = store.filter((s) => s.kind !== 'architect');
    const { unmount } = renderAt();
    await waitFor(() => expect(api.listChats).toHaveBeenCalled());

    const empty = await screen.findByRole('status');
    expect(within(empty).queryByRole('button')).toBeNull();
    expect(listedTitles()).toHaveLength(0);
    unmount();

    renderAt('/sessions?tab=chats');
    await screen.findByText('Fix login');
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'zzz' } });

    const noMatch = screen.getByRole('status');
    expect(listedTitles()).toHaveLength(0);
    fireEvent.click(within(noMatch).getByRole('button', { name: /clear filters/i }));

    expect(listedTitles()).toEqual(['Fix login']);
    expect(screen.getByRole('searchbox')).toHaveValue('');
  });
});
