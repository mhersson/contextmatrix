import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  AvailableChat,
  PaneSizes,
  PaneSlots,
  Slot,
} from '../components/ChatLayout/types';
import { DEFAULT_SIZES, EMPTY_PANES, SLOTS } from '../components/ChatLayout/types';

const PERSIST_KEY = 'chat_layout';
const PERSIST_DEBOUNCE_MS = 300;
const LAST_CHAT_ID_KEY = 'last_chat_id';

export interface ChatLayoutState {
  panes: PaneSlots;
  focused: Slot | null;
  sizes: PaneSizes;
  lastFocusedAt: Partial<Record<Slot, number>>;
}

export interface LRUEvictionEvent {
  victimSlot: Slot;
  victimChatId: string;
  incomingChatId: string;
  snapshot: ChatLayoutState;
}

export interface UseChatLayoutOptions {
  availableChats: AvailableChat[];
  onLRUEvict?: (event: LRUEvictionEvent) => void;
}

export interface UseChatLayoutResult {
  state: ChatLayoutState;
  occupied: Slot[];
  paneCount: number;
  draggingChatId: string | null;
  setDragging: (chatId: string | null) => void;
  focus: (slot: Slot) => void;
  openInFocused: (chatId: string) => void;
  openInNewPane: (chatId: string) => void;
  closePane: (slot: Slot) => void;
  swapPaneChat: (slot: Slot, chatId: string) => void;
  movePane: (fromSlot: Slot, toSlot: Slot) => void;
  splitFromPane: (slot: Slot) => void;
  cancelEmptyPane: (slot: Slot) => void;
  setSizes: (key: keyof PaneSizes, sizes: number[]) => void;
  restoreSnapshot: (snapshot: ChatLayoutState) => void;
  reset: () => void;
}

function emptyState(): ChatLayoutState {
  return {
    panes: { ...EMPTY_PANES },
    focused: null,
    sizes: { ...DEFAULT_SIZES },
    lastFocusedAt: {},
  };
}

function normalize(s: ChatLayoutState): ChatLayoutState {
  const panes: PaneSlots = { ...s.panes };
  let focused = s.focused;
  const lastFocusedAt = { ...s.lastFocusedAt };
  const remap = (from: Slot, to: Slot) => {
    if (focused === from) focused = to;
    if (lastFocusedAt[from] != null) {
      lastFocusedAt[to] = lastFocusedAt[from];
      delete lastFocusedAt[from];
    }
  };
  if (!panes.TL && panes.BL) { panes.TL = panes.BL; panes.BL = null; remap('BL', 'TL'); }
  if (!panes.TR && panes.BR) { panes.TR = panes.BR; panes.BR = null; remap('BR', 'TR'); }
  if (!panes.TL && !panes.BL && (panes.TR || panes.BR)) {
    if (panes.TR) { panes.TL = panes.TR; panes.TR = null; remap('TR', 'TL'); }
    if (panes.BR) { panes.BL = panes.BR; panes.BR = null; remap('BR', 'BL'); }
    if (!panes.TL && panes.BL) { panes.TL = panes.BL; panes.BL = null; remap('BL', 'TL'); }
  }
  const hasAny = SLOTS.some((slot) => panes[slot]);
  if (!hasAny) focused = null;
  else if (focused == null || !panes[focused]) {
    focused = SLOTS.find((slot) => panes[slot]) ?? null;
  }
  return { ...s, panes, focused, lastFocusedAt };
}

function isValidSize(n: unknown): n is number {
  return typeof n === 'number' && Number.isFinite(n) && n >= 0 && n <= 100;
}

function loadPersisted(): ChatLayoutState | null {
  // Does not filter against availableChats — that list comes from
  // useChatSessions which loads async, so it's empty on the first render
  // after ChatPage remounts (returning to /chat from another route).
  // Filtering here would drop every persisted pane and the debounced save
  // effect would then overwrite localStorage with empty state. The
  // reconciliation effect below removes stale ids once sessions resolve.
  try {
    const raw = window.localStorage.getItem(PERSIST_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<ChatLayoutState> | null;
    if (!parsed || typeof parsed !== 'object') return null;
    const panes: PaneSlots = { ...EMPTY_PANES };
    if (parsed.panes && typeof parsed.panes === 'object') {
      for (const slot of SLOTS) {
        const p = parsed.panes[slot];
        if (p && typeof p === 'object' && p.chatId) {
          panes[slot] = { chatId: p.chatId };
        }
      }
    }
    const sizes: PaneSizes = { ...DEFAULT_SIZES };
    if (parsed.sizes && typeof parsed.sizes === 'object') {
      if (isValidSize(parsed.sizes.col)) sizes.col = parsed.sizes.col;
      if (isValidSize(parsed.sizes.leftRow)) sizes.leftRow = parsed.sizes.leftRow;
      if (isValidSize(parsed.sizes.rightRow)) sizes.rightRow = parsed.sizes.rightRow;
    }
    const focused = parsed.focused && panes[parsed.focused] ? parsed.focused : null;
    const lastFocusedAt: Partial<Record<Slot, number>> = {};
    if (parsed.lastFocusedAt && typeof parsed.lastFocusedAt === 'object') {
      for (const slot of SLOTS) {
        const v = parsed.lastFocusedAt[slot];
        if (typeof v === 'number') lastFocusedAt[slot] = v;
      }
    }
    return normalize({ panes, focused, sizes, lastFocusedAt });
  } catch {
    return null;
  }
}

function savePersisted(state: ChatLayoutState): void {
  try {
    const payload: ChatLayoutState = {
      panes: state.panes,
      focused: state.focused,
      sizes: state.sizes,
      lastFocusedAt: state.lastFocusedAt,
    };
    window.localStorage.setItem(PERSIST_KEY, JSON.stringify(payload));
  } catch {
    // Storage disabled — non-fatal.
  }
}

function nextPlacementSlot(s: ChatLayoutState): Slot | null {
  const occupied = SLOTS.filter((slot) => s.panes[slot]);
  const count = occupied.length;
  if (count === 0) return 'TL';
  if (count === 1) return 'TR';
  if (count === 2) {
    const focusedInLeft = s.focused === 'TL' || s.focused === 'BL';
    return focusedInLeft ? 'BL' : 'BR';
  }
  if (count === 3) return SLOTS.find((slot) => !s.panes[slot]) ?? null;
  return null;
}

function splitTargetSlot(s: ChatLayoutState, originSlot: Slot): Slot | null {
  const occupied = SLOTS.filter((slot) => s.panes[slot]);
  const count = occupied.length;
  if (count >= 4) return null;
  if (count === 1) return 'TR';
  if (count === 2) {
    const originInLeft = originSlot === 'TL' || originSlot === 'BL';
    return originInLeft ? 'BL' : 'BR';
  }
  return SLOTS.find((slot) => !s.panes[slot]) ?? null;
}

function lruSlot(s: ChatLayoutState): Slot {
  let best: Slot | null = null;
  let bestStamp = Infinity;
  for (const slot of SLOTS) {
    if (!s.panes[slot]) continue;
    const stamp = s.lastFocusedAt[slot] ?? 0;
    if (stamp < bestStamp) { bestStamp = stamp; best = slot; }
  }
  if (!best) throw new Error('lruSlot called with no occupied panes');
  return best;
}

export function useChatLayout(options: UseChatLayoutOptions): UseChatLayoutResult {
  const { availableChats, onLRUEvict } = options;

  const availableIds = useMemo(
    () => new Set(availableChats.map((c) => c.id)),
    [availableChats],
  );

  const [state, setState] = useState<ChatLayoutState>(() => {
    const loaded = loadPersisted();
    return loaded ?? emptyState();
  });
  const [draggingChatId, setDraggingChatId] = useState<string | null>(null);

  // Reconcile against availableChats: if a chat is deleted server-side, drop it.
  // Tracks previous id set so we only run the reconciliation when it changes.
  // Initialised to an empty set (not the mount-time availableIds) so the
  // first non-empty observation reconciles stale persisted ids. Sessions
  // load async via useChatSessions, so availableIds is empty on mount when
  // ChatPage remounts; we skip reconciliation in that window (empty-equals-
  // empty early-return below) so persisted panes survive until sessions
  // confirm what's actually available.
  const prevIdsRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const prev = prevIdsRef.current;
    if (prev.size === availableIds.size && [...availableIds].every((id) => prev.has(id))) return;
    prevIdsRef.current = availableIds;
    setState((s) => {
      let dirty = false;
      const panes: PaneSlots = { ...s.panes };
      for (const slot of SLOTS) {
        const p = s.panes[slot];
        if (p?.chatId && !availableIds.has(p.chatId)) {
          panes[slot] = null;
          dirty = true;
        }
      }
      if (!dirty) return s;
      return normalize({ ...s, panes });
    });
  }, [availableIds]);

  // Persist (debounced)
  useEffect(() => {
    const t = setTimeout(() => savePersisted(state), PERSIST_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [state]);

  // Persist focused-pane chat id to last_chat_id. Tab title stays at the
  // index.html default ("ContextMatrix") — match the board view.
  useEffect(() => {
    const focusedChat = state.focused ? state.panes[state.focused]?.chatId : null;
    if (focusedChat) {
      try { window.localStorage.setItem(LAST_CHAT_ID_KEY, focusedChat); } catch { /* storage disabled */ }
    }
  }, [state.focused, state.panes]);

  const focus = useCallback((slot: Slot) => {
    setState((s) => {
      if (!s.panes[slot]) return s;
      return { ...s, focused: slot, lastFocusedAt: { ...s.lastFocusedAt, [slot]: Date.now() } };
    });
  }, []);

  const restoreSnapshot = useCallback((snapshot: ChatLayoutState) => {
    setState(normalize(snapshot));
  }, []);

  const reset = useCallback(() => {
    setState(emptyState());
  }, []);

  const openInNewPane = useCallback((chatId: string) => {
    setState((s) => {
      const existing = SLOTS.find((slot) => s.panes[slot]?.chatId === chatId);
      if (existing) {
        return { ...s, focused: existing, lastFocusedAt: { ...s.lastFocusedAt, [existing]: Date.now() } };
      }
      const occupied = SLOTS.filter((slot) => s.panes[slot]);
      if (occupied.length >= 4) {
        const victim = lruSlot(s);
        const victimChatId = s.panes[victim]?.chatId ?? null;
        const snapshot = s;
        const next: ChatLayoutState = {
          ...s,
          panes: { ...s.panes, [victim]: { chatId } },
          focused: victim,
          lastFocusedAt: { ...s.lastFocusedAt, [victim]: Date.now() },
        };
        if (victimChatId && onLRUEvict) {
          // Defer to next tick so the state commit lands before the toast appears.
          queueMicrotask(() => onLRUEvict({
            victimSlot: victim,
            victimChatId,
            incomingChatId: chatId,
            snapshot,
          }));
        }
        return next;
      }
      const target = nextPlacementSlot(s);
      if (!target) return s;
      return {
        ...s,
        panes: { ...s.panes, [target]: { chatId } },
        focused: target,
        lastFocusedAt: { ...s.lastFocusedAt, [target]: Date.now() },
      };
    });
  }, [onLRUEvict]);

  // openInFocused per the "always-new" build prompt is effectively the same
  // as openInNewPane (a single click → new pane, auto-tile). We expose both
  // names so the call sites stay readable; both route to the same logic.
  const openInFocused = openInNewPane;

  const closePane = useCallback((slot: Slot) => {
    setState((s) => {
      if (!s.panes[slot]) return s;
      const panes: PaneSlots = { ...s.panes, [slot]: null };
      const lastFocusedAt = { ...s.lastFocusedAt };
      delete lastFocusedAt[slot];
      const focused = s.focused === slot ? null : s.focused;
      return normalize({ ...s, panes, focused, lastFocusedAt });
    });
  }, []);

  const swapPaneChat = useCallback((slot: Slot, chatId: string) => {
    setState((s) => {
      if (!s.panes[slot]) {
        // dropped on an empty pane → just place
        return normalize({
          ...s,
          panes: { ...s.panes, [slot]: { chatId } },
          focused: slot,
          lastFocusedAt: { ...s.lastFocusedAt, [slot]: Date.now() },
        });
      }
      const existing = SLOTS.find((sl) => s.panes[sl]?.chatId === chatId);
      if (existing && existing !== slot) {
        // swap pane contents — the "swap" same-chat-twice policy
        const targetChat = s.panes[slot]!.chatId;
        const panes: PaneSlots = {
          ...s.panes,
          [slot]: { chatId },
          [existing]: targetChat ? { chatId: targetChat } : null,
        };
        return normalize({
          ...s,
          panes,
          focused: slot,
          lastFocusedAt: { ...s.lastFocusedAt, [slot]: Date.now() },
        });
      }
      if (existing && existing === slot) {
        // dropped on itself — no-op aside from focus
        return { ...s, focused: slot, lastFocusedAt: { ...s.lastFocusedAt, [slot]: Date.now() } };
      }
      // chat not currently open elsewhere — replace target's contents
      return {
        ...s,
        panes: { ...s.panes, [slot]: { chatId } },
        focused: slot,
        lastFocusedAt: { ...s.lastFocusedAt, [slot]: Date.now() },
      };
    });
  }, []);

  const movePane = useCallback((fromSlot: Slot, toSlot: Slot) => {
    if (fromSlot === toSlot) return;
    setState((s) => {
      const fromPane = s.panes[fromSlot];
      const toPane = s.panes[toSlot];
      // Unconditional swap — even if one side is null (source becomes empty,
      // target gets the chat). Normalize collapses any resulting empty slots.
      const panes: PaneSlots = {
        ...s.panes,
        [fromSlot]: toPane,
        [toSlot]: fromPane,
      };
      // Focus follows the dragged chat: set focused = toSlot when the source
      // had a chat (the common case). Stamp lastFocusedAt so LRU is correct.
      const hadChat = fromPane?.chatId != null;
      const focused = hadChat ? toSlot : s.focused;
      const lastFocusedAt = hadChat
        ? { ...s.lastFocusedAt, [toSlot]: Date.now() }
        : s.lastFocusedAt;
      return normalize({ ...s, panes, focused, lastFocusedAt });
    });
  }, []);

  const splitFromPane = useCallback((slot: Slot) => {
    setState((s) => {
      const target = splitTargetSlot(s, slot);
      if (!target) return s;
      return {
        ...s,
        panes: { ...s.panes, [target]: { chatId: null } },
        focused: target,
        lastFocusedAt: { ...s.lastFocusedAt, [target]: Date.now() },
      };
    });
  }, []);

  const cancelEmptyPane = useCallback((slot: Slot) => {
    setState((s) => {
      if (!s.panes[slot] || s.panes[slot]?.chatId != null) return s;
      const panes: PaneSlots = { ...s.panes, [slot]: null };
      const lastFocusedAt = { ...s.lastFocusedAt };
      delete lastFocusedAt[slot];
      return normalize({ ...s, panes, focused: null, lastFocusedAt });
    });
  }, []);

  const setSizes = useCallback((key: keyof PaneSizes, sizes: number[]) => {
    if (sizes.length < 1) return;
    const first = sizes[0];
    if (!isValidSize(first)) return;
    setState((s) => ({ ...s, sizes: { ...s.sizes, [key]: first } }));
  }, []);

  const setDragging = useCallback((chatId: string | null) => {
    setDraggingChatId(chatId);
  }, []);

  const occupied = useMemo(
    () => SLOTS.filter((slot) => state.panes[slot]),
    [state.panes],
  );

  return {
    state,
    occupied,
    paneCount: occupied.length,
    draggingChatId,
    setDragging,
    focus,
    openInFocused,
    openInNewPane,
    closePane,
    swapPaneChat,
    movePane,
    splitFromPane,
    cancelEmptyPane,
    setSizes,
    restoreSnapshot,
    reset,
  };
}
