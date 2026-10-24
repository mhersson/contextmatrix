import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import { SessionSections } from './SessionSections';
import { api } from '../../api/client';
import { CHAT_DRAG_START_EVENT } from '../ChatLayout/dragProtocol';
import type { ChatSession, SessionKind } from '../../types';

const theme = vi.hoisted(() => ({ architectEnabled: true }));
vi.mock('../../hooks/useTheme', () => ({ useTheme: () => theme }));

const toast = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('../../hooks/useToast', () => ({ useToast: () => toast }));

const drawer = vi.hoisted(() => ({ isOpen: true, toggle: () => {}, close: vi.fn() }));
vi.mock('../../context/MobileSidebarContext', () => ({ useMobileSidebar: () => drawer }));

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

const day = (n: number) => `2026-10-${String(n).padStart(2, '0')}T12:00:00Z`;

let store: ChatSession[] = [];

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

function renderSections(onNewSession: (kind: SessionKind) => void = () => {}) {
  return render(
    <MemoryRouter initialEntries={['/all']}>
      <SessionSections onNewSession={onNewSession} />
      <Where />
    </MemoryRouter>,
  );
}

const region = (name: string) => screen.getByRole('region', { name });
const rowTitles = (r: HTMLElement) => within(r).queryAllByRole('listitem').map((li) => li.textContent);
const newButton = (r: HTMLElement) => within(r).queryByRole('button', { name: /^New / });
const where = () => screen.getByTestId('where').textContent;

beforeEach(() => {
  theme.architectEnabled = true;
  toast.showToast.mockReset();
  drawer.close.mockReset();
  localStorage.clear();
  store = [
    sess({ id: 'c1', title: 'Fix login', last_active: day(3) }),
    sess({ id: 'c2', title: 'Triage', kind: 'chat', status: 'active', last_active: day(2) }),
    sess({ id: 'b1', title: 'Shopping list', kind: 'architect', last_active: day(4) }),
  ];
  vi.spyOn(api, 'listChats').mockImplementation(async () => store.map((s) => ({ ...s })));
  vi.spyOn(api, 'updateChat').mockImplementation(async (id, patch) => {
    store = store.map((s) => (s.id === id ? { ...s, pinned: patch.pinned } : s));
    return store.find((s) => s.id === id)!;
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('SessionSections', () => {
  it('splits chats and blueprints into their own sections, Chats first', async () => {
    renderSections();
    await screen.findByText('Shopping list');
    const regions = screen.getAllByRole('region');
    expect(regions).toHaveLength(2);
    const [first, second] = regions;
    expect(first).toBe(region('Chats'));
    expect(second).toBe(region('Blueprints'));
    expect(rowTitles(region('Chats'))).toEqual(['Triage', 'Fix login']);
    expect(rowTitles(region('Blueprints'))).toEqual(['Shopping list']);
    const bpRow = within(region('Blueprints')).getByRole('listitem');
    expect(within(bpRow).getByRole('img', { name: 'Blueprint session' })).toBeInTheDocument();
    for (const li of within(region('Chats')).getAllByRole('listitem')) {
      expect(within(li).queryByRole('img', { name: 'Blueprint session' })).not.toBeInTheDocument();
    }
  });

  it('ends each section with a link to its Sessions tab carrying the kind total', async () => {
    store = [
      ...Array.from({ length: 18 }, (_, i) => sess({ id: `c${i}`, last_active: day(i + 1) })),
      sess({ id: 'b1', kind: 'architect' }),
      sess({ id: 'b2', kind: 'architect' }),
    ];
    renderSections();
    await screen.findByText('c17');
    const chats = region('Chats');
    expect(within(chats).getAllByRole('listitem')).toHaveLength(5);
    const chatsLink = within(chats).getByRole('link');
    expect(chatsLink).toHaveAttribute('href', '/sessions?tab=chats');
    expect(chatsLink).toHaveTextContent('18');
    const bpLink = within(region('Blueprints')).getByRole('link');
    expect(bpLink).toHaveAttribute('href', '/sessions?tab=blueprints');
    expect(bpLink).toHaveTextContent('2');

    fireEvent.click(chatsLink);
    expect(where()).toBe('/sessions?tab=chats');
    expect(drawer.close).toHaveBeenCalled();
  });

  it('has no link in a section without sessions', async () => {
    store = [sess({ id: 'c1', title: 'Fix login' })];
    renderSections();
    await screen.findByText('Fix login');
    expect(within(region('Chats')).getByRole('link')).toBeInTheDocument();
    expect(within(region('Blueprints')).queryByRole('link')).not.toBeInTheDocument();
  });

  it('starts a session of the section kind from + new', async () => {
    const onNewSession = vi.fn();
    renderSections(onNewSession);
    await screen.findByText('Shopping list');
    fireEvent.click(newButton(region('Blueprints'))!);
    expect(onNewSession).toHaveBeenLastCalledWith('architect');
    fireEvent.click(newButton(region('Chats'))!);
    expect(onNewSession).toHaveBeenLastCalledWith('chat');
    expect(drawer.close).toHaveBeenCalledTimes(2);
  });

  it('hides Blueprints without an architect unless blueprint sessions exist, then without + new', async () => {
    theme.architectEnabled = false;
    store = [sess({ id: 'c1', title: 'Fix login' })];
    const { unmount } = renderSections();
    await screen.findByText('Fix login');
    expect(screen.queryByRole('region', { name: 'Blueprints' })).not.toBeInTheDocument();
    unmount();

    store = [sess({ id: 'c1', title: 'Fix login' }), sess({ id: 'b1', title: 'Old plan', kind: 'architect' })];
    renderSections();
    await screen.findByText('Old plan');
    expect(newButton(region('Blueprints'))).not.toBeInTheDocument();
    expect(newButton(region('Chats'))).toBeInTheDocument();
  });

  it('collapses each section on its own and remembers it under its own key', async () => {
    renderSections();
    await screen.findByText('Shopping list');
    const chatsToggle = within(region('Chats')).getByRole('button', { name: 'Chats' });
    fireEvent.click(chatsToggle);
    expect(chatsToggle).toHaveAttribute('aria-expanded', 'false');
    expect(rowTitles(region('Chats'))).toEqual([]);
    expect(rowTitles(region('Blueprints'))).toEqual(['Shopping list']);
    expect(localStorage.getItem('sidebar.chat_section_collapsed')).toBe('1');
    expect(localStorage.getItem('sidebar.blueprint_section_collapsed')).toBeNull();

    fireEvent.click(within(region('Blueprints')).getByRole('button', { name: 'Blueprints' }));
    expect(rowTitles(region('Blueprints'))).toEqual([]);
    expect(localStorage.getItem('sidebar.blueprint_section_collapsed')).toBe('1');
  });

  it('opens a section collapsed when its key says so', async () => {
    localStorage.setItem('sidebar.blueprint_section_collapsed', '1');
    renderSections();
    await screen.findByText('Fix login');
    expect(rowTitles(region('Blueprints'))).toEqual([]);
    expect(rowTitles(region('Chats'))).toEqual(['Triage', 'Fix login']);
  });

  it('pins from the pin button without opening the session, and the row moves to the top', async () => {
    renderSections();
    await screen.findByText('Fix login');
    const row = within(region('Chats')).getAllByRole('listitem')[1];
    const pin = within(row).getByRole('button', { name: /Fix login/, pressed: false });
    fireEvent.click(pin);
    await waitFor(() => expect(api.updateChat).toHaveBeenCalledWith('c1', { pinned: true }));
    expect(where()).toBe('/all');
    await waitFor(() => expect(rowTitles(region('Chats'))).toEqual(['Fix login', 'Triage']));
    expect(within(region('Chats')).getByRole('button', { name: /Fix login/, pressed: true })).toBeInTheDocument();
  });

  it('unpins a pinned row', async () => {
    store = [sess({ id: 'c1', title: 'Fix login', pinned: true })];
    renderSections();
    await screen.findByText('Fix login');
    fireEvent.click(within(region('Chats')).getByRole('button', { name: /Fix login/, pressed: true }));
    await waitFor(() => expect(api.updateChat).toHaveBeenCalledWith('c1', { pinned: false }));
  });

  it('opens the session as a pane on row click and closes the drawer', async () => {
    renderSections();
    fireEvent.click(await screen.findByRole('button', { name: 'Fix login' }));
    expect(where()).toBe('/chat/c1');
    expect(drawer.close).toHaveBeenCalled();
  });

  it('drags a row with its session id', async () => {
    renderSections();
    const open = await screen.findByRole('button', { name: 'Fix login' });
    const onStart = vi.fn();
    window.addEventListener(CHAT_DRAG_START_EVENT, onStart);
    const setData = vi.fn();
    fireEvent.dragStart(open, { dataTransfer: { setData, effectAllowed: '' } });
    window.removeEventListener(CHAT_DRAG_START_EVENT, onStart);
    expect(setData).toHaveBeenCalledWith('text/plain', 'c1');
    expect((onStart.mock.calls[0][0] as CustomEvent).detail).toEqual({ chatId: 'c1' });
  });
});
