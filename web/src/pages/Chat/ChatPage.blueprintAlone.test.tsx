/**
 * A blueprint shows alone across the chat area, driven through the real page,
 * layout and hook. Kept apart from ChatPage.test.tsx so these broad `vi.mock`s
 * don't bleed into the helper-component suite there.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useEffect } from 'react';
import { Link, MemoryRouter, Route, Routes } from 'react-router';
import { ChatPage } from './ChatPage';
import { api } from '../../api/client';
import { notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { CHAT_DRAG_END_EVENT, CHAT_DRAG_START_EVENT } from '../../components/ChatLayout/dragProtocol';
import type { ChatSession } from '../../types';

vi.mock('../../api/client', () => ({
  api: { listChats: vi.fn(), deleteChat: vi.fn(() => Promise.resolve()) },
  isAPIError: vi.fn(() => false),
  describeApiError: vi.fn(() => 'failed'),
}));

const threadMounts = vi.hoisted(() => [] as string[]);
vi.mock('./ChatThread', () => ({
  ChatThread: ({ sessionID }: { sessionID: string }) => {
    useEffect(() => {
      threadMounts.push(sessionID);
    }, [sessionID]);
    return <div data-testid="thread">{sessionID}</div>;
  },
}));
vi.mock('./NewChatDialog', () => ({ NewChatDialog: () => null }));
vi.mock('./MobileChatHeader', () => ({
  MobileChatHeader: ({ title }: { title: string }) => <div data-testid="mobile-title">{title}</div>,
}));
vi.mock('../../components/ConfirmModal/ConfirmModal', () => ({
  ConfirmModal: ({ open, variant, onConfirm }: { open: boolean; variant: string; onConfirm: () => void }) =>
    open && variant === 'danger' ? <button type="button" data-testid="confirm-delete" onClick={onConfirm} /> : null,
}));
vi.mock('../../hooks/useMediaQuery', () => ({ useMediaQuery: vi.fn(() => true) }));
vi.mock('../../hooks/useChatLiveData', () => ({ useChatLiveData: () => null }));
vi.mock('../../utils/chatModels', () => ({
  useChatModels: () => ({ models: [], source: 'endpoint' }),
  contextPct: () => 0,
  modelMaxTokens: () => 0,
  usageColor: () => 'var(--fg)',
  formatCostTooltip: () => '',
}));
vi.mock('../../components/ChatLayout/PaneAccentStripe', () => ({ PaneAccentStripe: () => null }));
vi.mock('../../utils/isTouchDevice', () => ({ isTouchDevice: () => false }));

function session(id: string, kind: 'chat' | 'architect'): ChatSession {
  return { id, title: id, kind, status: 'active' } as ChatSession;
}

const SESSIONS = [
  session('A', 'chat'),
  session('B', 'chat'),
  session('C', 'chat'),
  session('D', 'chat'),
  session('E', 'chat'),
  session('P', 'architect'),
  session('Q', 'architect'),
];

function saveLayout(panes: Partial<Record<'TL' | 'BL' | 'TR' | 'BR', string | null>>, focused = 'TL') {
  window.localStorage.setItem('chat_layout', JSON.stringify({
    panes: {
      TL: null, BL: null, TR: null, BR: null,
      ...Object.fromEntries(Object.entries(panes).map(([slot, chatId]) => [slot, { chatId }])),
    },
    focused,
    sizes: { col: 50, leftRow: 50, rightRow: 50 },
    lastFocusedAt: {},
  }));
}

function renderPage(entry = '/chat') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      {SESSIONS.map((s) => <Link key={s.id} to={`/chat/${s.id}`}>open {s.id}</Link>)}
      <Link to="/chat/NEW">open NEW</Link>
      <Routes>
        <Route path="chat" element={<ChatPage />} />
        <Route path="chat/:id" element={<ChatPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

function threads(): string[] {
  return screen.queryAllByTestId('thread').map((t) => t.textContent ?? '').sort();
}

async function showsThreads(...ids: string[]) {
  await waitFor(() => expect(threads()).toEqual([...ids].sort()));
}

function open(id: string) {
  fireEvent.click(screen.getByText(`open ${id}`));
}

function paneOf(id: string): HTMLElement {
  const pane = screen.getAllByTestId('thread').find((t) => t.textContent === id)?.closest<HTMLElement>('.chat-pane');
  if (!pane) throw new Error(`no pane shows ${id}`);
  return pane;
}

function dragOverPane(chatId: string, pane: HTMLElement) {
  act(() => {
    window.dispatchEvent(new CustomEvent(CHAT_DRAG_START_EVENT, { detail: { chatId } }));
  });
  fireEvent.dragOver(pane, { dataTransfer: { dropEffect: 'none' } });
}

function dropOnPane(chatId: string, pane: HTMLElement) {
  dragOverPane(chatId, pane);
  fireEvent.drop(pane, { dataTransfer: { getData: (type: string) => (type === 'text/plain' ? chatId : '') } });
  act(() => {
    window.dispatchEvent(new Event(CHAT_DRAG_END_EVENT));
  });
}

const closeButton = () => screen.getByRole('button', { name: 'Close pane' });

beforeEach(() => {
  threadMounts.length = 0;
  vi.mocked(api.listChats).mockReset();
  vi.mocked(api.listChats).mockResolvedValue(SESSIONS);
  vi.mocked(useMediaQuery).mockReturnValue(true);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('ChatPage - a blueprint shows alone', () => {
  it('shows alone from a sidebar link, then closing brings the chats back', async () => {
    saveLayout({ TL: 'A', TR: 'B' });
    renderPage();
    await showsThreads('A', 'B');

    open('P');
    await showsThreads('P');
    fireEvent.click(closeButton());
    await showsThreads('A', 'B');
  });

  it('offers no split on the blueprint but keeps it on a chat', async () => {
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    expect(screen.getByRole('button', { name: 'Split pane' })).toBeInTheDocument();

    open('P');
    await showsThreads('P');
    expect(screen.queryByRole('button', { name: 'Split pane' })).not.toBeInTheDocument();
  });

  it('replaces the blueprint showing with another one', async () => {
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    open('P');
    await showsThreads('P');
    open('Q');
    await showsThreads('Q');
  });

  it('opening a chat brings the grid back with it added', async () => {
    saveLayout({ TL: 'A', TR: 'B' });
    renderPage();
    await showsThreads('A', 'B');
    open('P');
    await showsThreads('P');
    open('C');
    await showsThreads('A', 'B', 'C');
  });

  it('opens over a full grid with no toast, and a chat opened after it shows the toast with a working Undo', async () => {
    saveLayout({ TL: 'A', TR: 'B', BL: 'C', BR: 'D' });
    renderPage();
    await showsThreads('A', 'B', 'C', 'D');

    open('P');
    await showsThreads('P');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();

    open('E');
    await waitFor(() => expect(threads()).toContain('E'));
    expect(threads()).toHaveLength(4);
    fireEvent.click(within(await screen.findByRole('status')).getByRole('button', { name: 'Undo' }));
    await showsThreads('P');
  });

  it('dropping a chat on the blueprint opens that chat beside the saved ones', async () => {
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    open('P');
    await showsThreads('P');
    dropOnPane('C', paneOf('P'));
    await showsThreads('A', 'C');
  });

  it('dropping a blueprint on a chat pane shows it alone and keeps the pane behind it', async () => {
    saveLayout({ TL: 'A', TR: 'B' });
    renderPage();
    await showsThreads('A', 'B');
    dropOnPane('P', paneOf('B'));
    await showsThreads('P');
    fireEvent.click(closeButton());
    await showsThreads('A', 'B');
  });

  it('the drop overlay promises an open rather than a swap when a blueprint is involved', async () => {
    saveLayout({ TL: 'A', TR: 'B' });
    renderPage();
    await showsThreads('A', 'B');

    const overlayNames = (chatId: string, pane: HTMLElement) => {
      dragOverPane(chatId, pane);
      const overlay = pane.querySelector<HTMLElement>('.chat-pane-drop-overlay');
      if (!overlay) throw new Error('no drop overlay');
      const named = within(overlay).queryByText(chatId) !== null;
      fireEvent.dragLeave(pane);
      return named;
    };

    expect(overlayNames('C', paneOf('A'))).toBe(false);
    expect(overlayNames('P', paneOf('A'))).toBe(true);
    open('P');
    await showsThreads('P');
    expect(overlayNames('C', paneOf('P'))).toBe(true);
  });

  it('picking a blueprint from an empty pane shows it alone and the empty pane waits behind it', async () => {
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    fireEvent.click(screen.getByRole('button', { name: 'Split pane' }));
    fireEvent.click(await screen.findByRole('button', { name: /^P/ }));
    await showsThreads('P');

    fireEvent.click(closeButton());
    await showsThreads('A');
    expect(screen.getAllByRole('button', { name: 'Close pane' })).toHaveLength(2);
  });

  it('deleting the showing blueprint brings the chats back', async () => {
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    open('P');
    await showsThreads('P');

    fireEvent.click(screen.getByRole('button', { name: 'More chat actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete chat' }));
    vi.mocked(api.listChats).mockResolvedValue(SESSIONS.filter((s) => s.id !== 'P'));
    fireEvent.click(await screen.findByTestId('confirm-delete'));

    await showsThreads('A');
    expect(api.deleteChat).toHaveBeenCalledWith('P');
  });

  it('a blueprint deleted elsewhere brings the chats back on the next list', async () => {
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    open('P');
    await showsThreads('P');

    vi.mocked(api.listChats).mockResolvedValue(SESSIONS.filter((s) => s.id !== 'P'));
    act(() => notifyChatSessionsChanged());
    await showsThreads('A');
  });

  it('a just-created blueprint not yet listed stays alone over a full grid with no toast', async () => {
    saveLayout({ TL: 'A', TR: 'B', BL: 'C', BR: 'D' });
    renderPage();
    await showsThreads('A', 'B', 'C', 'D');

    open('NEW');
    await showsThreads('NEW');
    vi.mocked(api.listChats).mockResolvedValue([...SESSIONS, session('NEW', 'architect')]);
    act(() => notifyChatSessionsChanged());
    await waitFor(() => expect(api.listChats).toHaveBeenCalledTimes(2));
    await showsThreads('NEW');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();

    fireEvent.click(closeButton());
    await showsThreads('A', 'B', 'C', 'D');
  });

  it('shows a blueprint on a phone', async () => {
    vi.mocked(useMediaQuery).mockReturnValue(false);
    saveLayout({ TL: 'A' });
    renderPage();
    await showsThreads('A');
    open('P');
    await showsThreads('P');
    expect(screen.getByTestId('mobile-title').textContent).toBe('P');
    open('A');
    await showsThreads('A');
  });
});

describe('ChatPage - opening before the session list has loaded', () => {
  function holdList() {
    let release: (list: ChatSession[]) => void = () => {};
    vi.mocked(api.listChats).mockReturnValue(new Promise((resolve) => { release = resolve; }));
    return (list: ChatSession[]) => act(async () => { release(list); });
  }

  it('a cold link to a blueprint mounts no saved chat and shows it alone once resolved', async () => {
    saveLayout({ TL: 'A', TR: 'B', BL: 'C', BR: 'D' });
    const release = holdList();
    renderPage('/chat/P');
    await release(SESSIONS);
    await showsThreads('P');
    expect(threadMounts).toEqual(['P']);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('a cold link to a chat mounts each pane once, in the grid', async () => {
    saveLayout({ TL: 'A' });
    const release = holdList();
    renderPage('/chat/B');
    await release(SESSIONS);
    await showsThreads('A', 'B');
    expect([...threadMounts].sort()).toEqual(['A', 'B']);
  });

  it('a cold reload with a blueprint in an old saved layout moves it out and leaves the chats saved', async () => {
    saveLayout({ TL: 'A', TR: 'P' }, 'TR');
    renderPage();
    await showsThreads('P');
    fireEvent.click(closeButton());
    await showsThreads('A');
  });
});
