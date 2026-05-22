import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatLayout, type ChatLayoutState, type LRUEvictionEvent } from './useChatLayout';
import type { AvailableChat } from '../components/ChatLayout/types';

// localStorage isn't auto-provided in this test environment — mirror the
// pattern used in useAgentId.test.ts so the hook's persistence code runs.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();
Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, writable: true });

const CHATS: AvailableChat[] = [
  { id: 'A', title: 'a' },
  { id: 'B', title: 'b' },
  { id: 'C', title: 'c' },
  { id: 'D', title: 'd' },
  { id: 'E', title: 'e' },
];

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

function renderLayout(overrides: { onLRUEvict?: (e: LRUEvictionEvent) => void } = {}) {
  return renderHook(() => useChatLayout({ availableChats: CHATS, ...overrides }));
}

describe('useChatLayout — placement', () => {
  it('starts empty', () => {
    const { result } = renderLayout();
    expect(result.current.paneCount).toBe(0);
    expect(result.current.state.focused).toBeNull();
  });

  it('places 1→2→3→4 in TL, TR, BL, BR with the focused-column split rule', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.focused).toBe('TL');

    act(() => { result.current.openInNewPane('B'); });
    expect(result.current.state.panes.TR?.chatId).toBe('B');
    expect(result.current.state.focused).toBe('TR');

    // Focused is TR → splits the RIGHT column → new pane in BR
    act(() => { result.current.openInNewPane('C'); });
    expect(result.current.state.panes.BR?.chatId).toBe('C');
    expect(result.current.state.focused).toBe('BR');

    // 4th fills the only remaining slot (BL)
    act(() => { result.current.openInNewPane('D'); });
    expect(result.current.state.panes.BL?.chatId).toBe('D');
    expect(result.current.paneCount).toBe(4);
  });

  it('opening an already-open chat focuses its slot instead of creating a new pane', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.focus('TL'); });
    expect(result.current.state.focused).toBe('TL');

    act(() => { result.current.openInNewPane('B'); });
    expect(result.current.paneCount).toBe(2);
    expect(result.current.state.focused).toBe('TR');
  });

  it('splits the LEFT column when the focused pane is on the left', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.focus('TL'); });
    act(() => { result.current.openInNewPane('C'); });
    expect(result.current.state.panes.BL?.chatId).toBe('C');
    expect(result.current.state.panes.BR).toBeNull();
  });
});

describe('useChatLayout — LRU eviction', () => {
  it('evicts the least-recently-focused pane when opening a 5th chat', () => {
    const evictions: LRUEvictionEvent[] = [];
    const { result } = renderLayout({ onLRUEvict: (e) => evictions.push(e) });
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.openInNewPane('C'); });
    act(() => { result.current.openInNewPane('D'); });
    expect(result.current.paneCount).toBe(4);

    // A was focused first, then B, C, D took focus. A's lastFocusedAt is oldest.
    // (each openInNewPane stamps the new pane's focus.)
    act(() => { result.current.openInNewPane('E'); });

    // microtask queue must flush for the onLRUEvict callback
    return Promise.resolve().then(() => {
      expect(evictions).toHaveLength(1);
      expect(evictions[0].victimChatId).toBe('A');
      expect(evictions[0].incomingChatId).toBe('E');
      const slot = evictions[0].victimSlot;
      expect(result.current.state.panes[slot]?.chatId).toBe('E');
    });
  });

  it('restoreSnapshot reverses an eviction', () => {
    const evictions: LRUEvictionEvent[] = [];
    const { result } = renderLayout({ onLRUEvict: (e) => evictions.push(e) });
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.openInNewPane('C'); });
    act(() => { result.current.openInNewPane('D'); });
    act(() => { result.current.openInNewPane('E'); });
    return Promise.resolve().then(() => {
      const snap = evictions[0].snapshot;
      act(() => { result.current.restoreSnapshot(snap); });
      const hasA = Object.values(result.current.state.panes).some(p => p?.chatId === 'A');
      const hasE = Object.values(result.current.state.panes).some(p => p?.chatId === 'E');
      expect(hasA).toBe(true);
      expect(hasE).toBe(false);
    });
  });
});

describe('useChatLayout — swapPaneChat (drop semantics)', () => {
  it('swaps two panes when dropping a chat already open in another pane', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.openInNewPane('B'); }); // TR
    // Drop A onto TR → swap: TL becomes B, TR becomes A
    act(() => { result.current.swapPaneChat('TR', 'A'); });
    expect(result.current.state.panes.TL?.chatId).toBe('B');
    expect(result.current.state.panes.TR?.chatId).toBe('A');
    expect(result.current.state.focused).toBe('TR');
  });

  it('replaces target with chat when chat is not open elsewhere', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.swapPaneChat('TR', 'C'); });
    expect(result.current.state.panes.TR?.chatId).toBe('C');
    expect(result.current.paneCount).toBe(2);
  });

  it('places into empty pane (post-split) when dropped there', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.splitFromPane('TR'); });
    const emptySlot = (['BL', 'BR'] as const).find(
      (s) => result.current.state.panes[s]?.chatId == null && result.current.state.panes[s] != null,
    )!;
    act(() => { result.current.swapPaneChat(emptySlot, 'C'); });
    expect(result.current.state.panes[emptySlot]?.chatId).toBe('C');
  });

  it('is a no-op (aside from focus) when dropping on the pane already holding the chat', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.swapPaneChat('TL', 'A'); });
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR?.chatId).toBe('B');
    expect(result.current.state.focused).toBe('TL');
  });
});

describe('useChatLayout — split / cancel empty', () => {
  it('splitFromPane creates an empty pane next to the source', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.splitFromPane('TL'); });
    expect(result.current.state.panes.TR).not.toBeNull();
    expect(result.current.state.panes.TR?.chatId).toBeNull();
    expect(result.current.state.focused).toBe('TR');
  });

  it('cancelEmptyPane removes only an empty pane', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.splitFromPane('TL'); });
    expect(result.current.paneCount).toBe(2);
    act(() => { result.current.cancelEmptyPane('TR'); });
    expect(result.current.paneCount).toBe(1);
    expect(result.current.state.panes.TR).toBeNull();
  });

  it('cancelEmptyPane is a no-op on a non-empty pane', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.cancelEmptyPane('TL'); });
    expect(result.current.state.panes.TL?.chatId).toBe('A');
  });
});

describe('useChatLayout — close + normalize', () => {
  it('closing TL promotes BL to TL', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.focus('TL'); });
    act(() => { result.current.openInNewPane('C'); }); // BL
    act(() => { result.current.closePane('TL'); });
    expect(result.current.state.panes.TL?.chatId).toBe('C');
    expect(result.current.state.panes.BL).toBeNull();
  });

  it('closing the only pane returns to empty state', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.closePane('TL'); });
    expect(result.current.paneCount).toBe(0);
    expect(result.current.state.focused).toBeNull();
  });

  it('closing the focused pane transfers focus to another occupied slot', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    act(() => { result.current.closePane('TR'); });
    expect(result.current.state.focused).toBe('TL');
  });

  it('closing the only right-column pane redistributes BL → TR so both columns stay populated', () => {
    // Reproduces the blank-page state where TL+BL filled with TR+BR null
    // hits ChatLayout's !hasRight return-null branch. The hook must keep
    // the layout valid by spreading the two surviving panes across both
    // columns instead of leaving one column empty.
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.openInNewPane('B'); }); // TR
    act(() => { result.current.focus('TL'); });
    act(() => { result.current.openInNewPane('C'); }); // BL (left-column split)
    act(() => { result.current.closePane('TR'); });
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR?.chatId).toBe('C');
    expect(result.current.state.panes.BL).toBeNull();
    expect(result.current.state.panes.BR).toBeNull();
    expect(result.current.state.focused).toBe('TR');
  });
});

describe('useChatLayout — persistence', () => {
  it('writes state to localStorage (after debounce)', async () => {
    vi.useFakeTimers();
    try {
      const { result } = renderLayout();
      act(() => { result.current.openInNewPane('A'); });
      act(() => { vi.advanceTimersByTime(400); });
      const raw = window.localStorage.getItem('chat_layout');
      expect(raw).not.toBeNull();
      const parsed = JSON.parse(raw!);
      expect(parsed.panes.TL.chatId).toBe('A');
      expect(parsed.focused).toBe('TL');
    } finally {
      vi.useRealTimers();
    }
  });

  it('rehydrates from localStorage on mount', () => {
    const persisted: ChatLayoutState = {
      panes: { TL: { chatId: 'A' }, BL: null, TR: { chatId: 'B' }, BR: null },
      focused: 'TR',
      sizes: { col: 30, leftRow: 50, rightRow: 50 },
      lastFocusedAt: { TL: 1000, TR: 2000 },
    };
    window.localStorage.setItem('chat_layout', JSON.stringify(persisted));
    const { result } = renderLayout();
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR?.chatId).toBe('B');
    expect(result.current.state.focused).toBe('TR');
    expect(result.current.state.sizes.col).toBe(30);
  });

  it('rehydrating from TL+BL with empty right column promotes BL → TR', () => {
    // Real user state observed in production: layout persisted with
    // TL+BL filled and TR+BR null, focus on BL. Without normalization
    // ChatLayout returns null (no right column) and the page is blank.
    window.localStorage.setItem('chat_layout', JSON.stringify({
      panes: { TL: { chatId: 'A' }, BL: { chatId: 'B' }, TR: null, BR: null },
      focused: 'BL',
      sizes: { col: 33, leftRow: 26, rightRow: 50 },
      lastFocusedAt: { TL: 1000, BL: 2000 },
    }));
    const { result } = renderLayout();
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR?.chatId).toBe('B');
    expect(result.current.state.panes.BL).toBeNull();
    expect(result.current.state.panes.BR).toBeNull();
    expect(result.current.state.focused).toBe('TR');
  });

  it('drops persisted ids not in availableChats during rehydration', () => {
    window.localStorage.setItem('chat_layout', JSON.stringify({
      panes: { TL: { chatId: 'A' }, BL: null, TR: { chatId: 'GONE' }, BR: null },
      focused: 'TR',
      sizes: { col: 50, leftRow: 50, rightRow: 50 },
      lastFocusedAt: {},
    }));
    const { result } = renderLayout();
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR).toBeNull();
    // focused was TR → unavailable → normalized to TL
    expect(result.current.state.focused).toBe('TL');
  });

  it('writes last_chat_id for the focused pane', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); });
    expect(window.localStorage.getItem('last_chat_id')).toBe('A');
    act(() => { result.current.openInNewPane('B'); });
    expect(window.localStorage.getItem('last_chat_id')).toBe('B');
  });
});

describe('useChatLayout — movePane', () => {
  it('movePane swaps two non-empty slots', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.openInNewPane('B'); }); // TR
    act(() => { result.current.movePane('TL', 'TR'); });
    expect(result.current.state.panes.TL?.chatId).toBe('B');
    expect(result.current.state.panes.TR?.chatId).toBe('A');
    // Focus follows the dragged chat (source had A, so focused = toSlot = TR)
    expect(result.current.state.focused).toBe('TR');
  });

  it('movePane from non-empty to empty slot: target gets chat, source becomes empty pane', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.splitFromPane('TL'); }); // TR (empty pane placeholder)
    // Before: TL = {chatId: 'A'}, TR = {chatId: null}
    act(() => { result.current.movePane('TL', 'TR'); });
    // After swap: TL = {chatId: null}, TR = {chatId: 'A'}.
    // normalize() doesn't promote TR → TL because TL is an empty pane object (not null).
    expect(result.current.state.panes.TR?.chatId).toBe('A');
    expect(result.current.state.panes.TL?.chatId).toBeNull(); // empty pane placeholder
    // Chat A is now in TR, focused = TR (source had a chat).
    expect(result.current.state.focused).toBe('TR');
  });

  it('movePane is a no-op when fromSlot === toSlot', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.openInNewPane('B'); }); // TR
    const before = result.current.state;
    act(() => { result.current.movePane('TL', 'TL'); });
    expect(result.current.state).toBe(before);
  });

  it('movePane on two columns preserves layout shape (2 panes → swap left↔right)', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.openInNewPane('B'); }); // TR
    expect(result.current.paneCount).toBe(2);
    act(() => { result.current.movePane('TL', 'TR'); });
    expect(result.current.paneCount).toBe(2);
    expect(result.current.state.panes.TL?.chatId).toBe('B');
    expect(result.current.state.panes.TR?.chatId).toBe('A');
  });

  it('movePane from empty-pane-object source to non-empty target does NOT change focus or stamp LRU', () => {
    const { result } = renderLayout();
    act(() => { result.current.openInNewPane('A'); }); // TL
    act(() => { result.current.openInNewPane('B'); }); // TR
    // Create an empty pane in BL via splitFromPane from TL
    act(() => { result.current.focus('TL'); });
    act(() => { result.current.splitFromPane('TL'); }); // creates empty pane in BL
    const emptySlot = (['BL', 'BR'] as const).find(
      (s) => result.current.state.panes[s]?.chatId == null && result.current.state.panes[s] != null,
    )!;
    // Make TL focused (non-empty target)
    act(() => { result.current.focus('TL'); });
    const focusedBefore = result.current.state.focused;
    const lruBefore = result.current.state.lastFocusedAt['TL'];

    // Move from the empty pane slot to TL
    act(() => { result.current.movePane(emptySlot, 'TL'); });

    // focused must be unchanged (source had no chatId)
    expect(result.current.state.focused).toBe(focusedBefore);
    // lastFocusedAt for TL must be unchanged (no LRU stamp)
    expect(result.current.state.lastFocusedAt['TL']).toBe(lruBefore);
  });
});

describe('useChatLayout — reconciliation against availableChats', () => {
  it('drops panes whose chat is removed from availableChats', () => {
    const { result, rerender } = renderHook(
      ({ chats }) => useChatLayout({ availableChats: chats }),
      { initialProps: { chats: CHATS } },
    );
    act(() => { result.current.openInNewPane('A'); });
    act(() => { result.current.openInNewPane('B'); });
    expect(result.current.paneCount).toBe(2);
    // Remove 'B' from server-side list
    rerender({ chats: CHATS.filter((c) => c.id !== 'B') });
    expect(result.current.state.panes.TR).toBeNull();
    expect(result.current.paneCount).toBe(1);
  });

  it('keeps persisted panes when availableChats is initially empty (async session load)', () => {
    // Mirrors the real ChatPage remount sequence: useChatSessions fetches in a
    // useEffect, so the first render sees availableChats=[]. The persisted
    // layout must survive that window — reconciliation should only remove
    // panes once sessions confirm they're gone, not before they've loaded.
    window.localStorage.setItem('chat_layout', JSON.stringify({
      panes: { TL: { chatId: 'A' }, BL: null, TR: { chatId: 'B' }, BR: null },
      focused: 'TR',
      sizes: { col: 50, leftRow: 50, rightRow: 50 },
      lastFocusedAt: { TL: 1000, TR: 2000 },
    }));
    const { result, rerender } = renderHook(
      ({ chats }: { chats: AvailableChat[] }) => useChatLayout({ availableChats: chats }),
      { initialProps: { chats: [] as AvailableChat[] } },
    );
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR?.chatId).toBe('B');
    expect(result.current.state.focused).toBe('TR');
    rerender({ chats: CHATS });
    expect(result.current.state.panes.TL?.chatId).toBe('A');
    expect(result.current.state.panes.TR?.chatId).toBe('B');
    expect(result.current.state.focused).toBe('TR');
  });
});
