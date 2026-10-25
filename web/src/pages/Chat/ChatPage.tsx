import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router';
import { NewChatDialog } from './NewChatDialog';
import { ChatThread } from './ChatThread';
import { MobileChatHeader } from './MobileChatHeader';
import { ChatLayout, type AvailableChat, type Slot } from '../../components/ChatLayout';
import { useChatLayout, type ChatLayoutState, type LRUEvictionEvent } from '../../hooks/useChatLayout';
import { useChatSessions, notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { api, describeApiError } from '../../api/client';
import { safeRemove } from '../../utils/safeStorage';
import { ConfirmModal } from '../../components/ConfirmModal/ConfirmModal';
import type { HandoffPrefill } from '../../components/ChatPanel/handoff';
import type { SessionKind } from '../../types';
import { HandoffContext, type HandoffContextValue } from '../../components/ChatPanel/handoffContext';
import {
  CHAT_DRAG_START_EVENT,
  CHAT_DRAG_END_EVENT,
} from '../../components/ChatLayout/dragProtocol';

interface EvictToast {
  victimChatId: string;
  incomingChatId: string;
  snapshot: ChatLayoutState;
}

interface ClearErrorToast {
  chatId: string;
  message: string;
  /** Human-readable verb shown in the toast, e.g. "clear context", "end session", "reopen". */
  action?: string;
}

export function ChatPage() {
  const { id: deepLinkId } = useParams();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const newParam = params.get('new');
  const wantsNew: SessionKind | null =
    newParam === 'blueprint' ? 'architect' : newParam === '1' ? 'chat' : null;
  const [dialogOpen, setDialogOpen] = useState(wantsNew !== null);
  const [dialogKind, setDialogKind] = useState<SessionKind>(wantsNew ?? 'chat');
  const [handoff, setHandoff] = useState<HandoffPrefill | null>(null);

  const { sessions } = useChatSessions();
  const isDesktop = useMediaQuery('(min-width: 768px)');
  const isMobile = !isDesktop;

  const availableChats = useMemo<AvailableChat[]>(
    () => sessions.map((s) => ({
      id: s.id,
      title: s.title || '(untitled)',
      status: s.status,
      model: s.model,
      kind: s.kind,
      mode: s.mode,
      cost_cap_usd: s.cost_cap_usd,
      plain_language: s.plain_language,
      unattended: s.unattended,
    })),
    [sessions],
  );

  const [evictToast, setEvictToast] = useState<EvictToast | null>(null);
  const evictTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const dismissEvictToast = useCallback(() => {
    if (evictTimerRef.current) {
      clearTimeout(evictTimerRef.current);
      evictTimerRef.current = null;
    }
    setEvictToast(null);
  }, []);
  useEffect(() => () => { if (evictTimerRef.current) clearTimeout(evictTimerRef.current); }, []);

  const [clearErrorToast, setClearErrorToast] = useState<ClearErrorToast | null>(null);
  const clearErrorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const dismissClearErrorToast = useCallback(() => {
    if (clearErrorTimerRef.current) {
      clearTimeout(clearErrorTimerRef.current);
      clearErrorTimerRef.current = null;
    }
    setClearErrorToast(null);
  }, []);
  useEffect(() => () => { if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current); }, []);

  const onLRUEvict = useCallback((e: LRUEvictionEvent) => {
    if (evictTimerRef.current) clearTimeout(evictTimerRef.current);
    setEvictToast({
      victimChatId: e.victimChatId,
      incomingChatId: e.incomingChatId,
      snapshot: e.snapshot,
    });
    evictTimerRef.current = setTimeout(() => setEvictToast(null), 6000);
  }, []);

  const layout = useChatLayout({ availableChats, onLRUEvict });

  // Sidebar fires custom drag events with the chat id so the pane drop
  // overlay can show the chat name. We use window events because the sidebar's
  // session rows live outside the layout subtree.
  useEffect(() => {
    const onStart = (e: Event) => {
      const detail = (e as CustomEvent<{ chatId?: string }>).detail;
      if (detail?.chatId) layout.setDragging(detail.chatId);
    };
    const onEnd = () => layout.setDragging(null);
    window.addEventListener(CHAT_DRAG_START_EVENT, onStart);
    window.addEventListener(CHAT_DRAG_END_EVENT, onEnd);
    return () => {
      window.removeEventListener(CHAT_DRAG_START_EVENT, onStart);
      window.removeEventListener(CHAT_DRAG_END_EVENT, onEnd);
    };
  }, [layout]);

  // /chat/:id deep links: open the chat as a new pane on top of the hydrated
  // layout, then bounce back to /chat so refresh doesn't re-fire. The open is
  // an in-render marker (CardPanel idiom); the bounce is an effect because
  // navigate() during render is a no-op. The marker starts undefined so it
  // fires on a fresh mount at /chat/:id (a mobile drawer click).
  const [prevDeepLinkId, setPrevDeepLinkId] = useState<string | undefined>(undefined);
  if (deepLinkId && deepLinkId !== prevDeepLinkId) {
    setPrevDeepLinkId(deepLinkId);
    layout.openInNewPane(deepLinkId);
  } else if (!deepLinkId && prevDeepLinkId) {
    setPrevDeepLinkId(undefined);
  }
  useEffect(() => {
    if (deepLinkId) navigate('/chat', { replace: true });
  }, [deepLinkId, navigate]);

  // ?new=1|blueprint → open NewChatDialog on that kind (in-render reset pattern)
  const [prevWantsNew, setPrevWantsNew] = useState(wantsNew);
  if (wantsNew !== prevWantsNew) {
    setPrevWantsNew(wantsNew);
    if (wantsNew) {
      setDialogKind(wantsNew);
      setDialogOpen(true);
    }
  }

  const openHandoff = useCallback((prefill: HandoffPrefill) => {
    setHandoff(prefill);
    setDialogKind('chat');
    setDialogOpen(true);
  }, []);
  const handoffValue = useMemo<HandoffContextValue>(() => ({ sessions, openHandoff }), [sessions, openHandoff]);

  const openNewChat = () => {
    setDialogKind('chat');
    setDialogOpen(true);
  };

  const closeDialog = () => {
    setDialogOpen(false);
    setHandoff(null);
    if (params.get('new')) {
      const next = new URLSearchParams(params);
      next.delete('new');
      setParams(next, { replace: true });
    }
  };

  const handleResize = useCallback(
    (key: 'col' | 'leftRow' | 'rightRow', sizes: number[]) => layout.setSizes(key, sizes),
    [layout],
  );
  const handleDropChatOnPane = useCallback(
    (slot: Parameters<typeof layout.swapPaneChat>[0], chatId: string) => {
      layout.setDragging(null);
      layout.swapPaneChat(slot, chatId);
    },
    [layout],
  );

  const handleMovePane = useCallback(
    (fromSlot: Slot, toSlot: Slot) => {
      layout.setDragging(null);
      layout.movePane(fromSlot, toSlot);
    },
    [layout],
  );

  const renderPaneBody = useCallback(
    (chatId: string, _slot: Slot, isFocused: boolean) => (
      <ChatThread sessionID={chatId} embedded isFocused={isFocused} phone={isMobile} />
    ),
    [isMobile],
  );

  // Per-pane chat actions (End / Reopen / Delete). Each calls the API,
  // notifies the sidebar so the status dot reflects the new state, and
  // for Delete also closes the pane that hosted the chat.
  const handleEndSession = useCallback(async (chatId: string) => {
    try {
      await api.endChat(chatId);
    } catch (e) {
      const message = describeApiError(e, 'Request failed');
      if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current);
      setClearErrorToast({ chatId, message, action: 'end session' });
      clearErrorTimerRef.current = setTimeout(() => setClearErrorToast(null), 6000);
    } finally {
      notifyChatSessionsChanged();
    }
  }, []);

  const handleReopenChat = useCallback(async (chatId: string) => {
    try {
      await api.openChat(chatId);
    } catch (e) {
      const message = describeApiError(e, 'Request failed');
      if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current);
      setClearErrorToast({ chatId, message, action: 'reopen' });
      clearErrorTimerRef.current = setTimeout(() => setClearErrorToast(null), 6000);
    } finally {
      notifyChatSessionsChanged();
    }
  }, []);

  const [pendingClear, setPendingClear] = useState<{ chatId: string } | null>(null);
  const requestClearContext = useCallback((chatId: string) => {
    setPendingClear({ chatId });
  }, []);
  const cancelClear = useCallback(() => setPendingClear(null), []);
  const confirmClearContext = useCallback(async () => {
    if (!pendingClear) return;
    const { chatId } = pendingClear;
    try {
      await api.clearChatContext(chatId);
    } catch (e) {
      console.warn('clearChatContext failed', e);
      const message = describeApiError(e, 'Request failed');
      if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current);
      setClearErrorToast({ chatId, message });
      clearErrorTimerRef.current = setTimeout(() => setClearErrorToast(null), 6000);
    } finally {
      setPendingClear(null);
    }
  }, [pendingClear]);

  const [pendingDelete, setPendingDelete] = useState<{ chatId: string; slot: Slot } | null>(null);
  const requestDeleteChat = useCallback((slot: Slot, chatId: string) => {
    setPendingDelete({ chatId, slot });
  }, []);
  const cancelDelete = useCallback(() => setPendingDelete(null), []);
  const confirmDelete = useCallback(async () => {
    if (!pendingDelete) return;
    const { chatId, slot } = pendingDelete;
    try {
      await api.deleteChat(chatId);
      layout.closePane(slot);
      safeRemove('last_chat_id');
    } catch (e) {
      const message = describeApiError(e, 'Request failed');
      if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current);
      setClearErrorToast({ chatId, message, action: 'delete chat' });
      clearErrorTimerRef.current = setTimeout(() => setClearErrorToast(null), 6000);
    } finally {
      notifyChatSessionsChanged();
      setPendingDelete(null);
    }
  }, [pendingDelete, layout]);

  const focusedChatId = useMemo(
    () => (layout.state.focused ? layout.state.panes[layout.state.focused]?.chatId ?? null : null),
    [layout.state.focused, layout.state.panes],
  );
  const mobileTitle = useMemo(
    () => (focusedChatId
      ? availableChats.find((c) => c.id === focusedChatId)?.title ?? 'Chat'
      : 'Chats'),
    [focusedChatId, availableChats],
  );

  return (
    <>
      <div className="flex flex-col h-full">
        {isMobile && (
          <MobileChatHeader
            title={mobileTitle}
            onNewChat={openNewChat}
          />
        )}
        <div className="flex-1 min-h-0">
          <HandoffContext.Provider value={handoffValue}>
            <ChatLayout
              panes={layout.state.panes}
              focused={layout.state.focused}
              sizes={layout.state.sizes}
              availableChats={availableChats}
              draggingChatId={layout.draggingChatId}
              isMobile={isMobile}
              onFocus={layout.focus}
              onClose={layout.closePane}
              onSplit={layout.splitFromPane}
              onResize={handleResize}
              onDropChatOnPane={handleDropChatOnPane}
              onMovePane={handleMovePane}
              onPickEmptyPane={layout.swapPaneChat}
              onCancelEmpty={layout.cancelEmptyPane}
              onNewChat={openNewChat}
              onEndSession={handleEndSession}
              onClearContext={requestClearContext}
              onReopenChat={handleReopenChat}
              onDeleteChat={requestDeleteChat}
              renderPaneBody={renderPaneBody}
            />
          </HandoffContext.Provider>
        </div>
      </div>
      {evictToast && (
        <div className="chat-evict-toast" role="status" aria-live="polite">
          <span className="chat-evict-toast-text">
            Closed <strong>{evictToast.victimChatId}</strong> to make room for <strong>{evictToast.incomingChatId}</strong>.
          </span>
          <button
            type="button"
            className="chat-evict-toast-btn"
            onClick={() => {
              layout.restoreSnapshot(evictToast.snapshot);
              dismissEvictToast();
            }}
          >Undo</button>
          <button
            type="button"
            className="chat-evict-toast-close"
            aria-label="Dismiss"
            onClick={dismissEvictToast}
          >×</button>
        </div>
      )}
      {clearErrorToast && (
        <div className="chat-evict-toast" role="alert" aria-live="assertive">
          <span className="chat-evict-toast-text">
            Couldn&apos;t {clearErrorToast.action ?? 'clear context'} for <strong>{availableChats.find((c) => c.id === clearErrorToast.chatId)?.title ?? 'this chat'}</strong>: {clearErrorToast.message}.
          </span>
          <button
            type="button"
            className="chat-evict-toast-close"
            aria-label="Dismiss"
            onClick={dismissClearErrorToast}
          >×</button>
        </div>
      )}
      <NewChatDialog open={dialogOpen} onClose={closeDialog} initialKind={dialogKind} handoff={handoff ?? undefined} />
      <ConfirmModal
        open={pendingClear !== null}
        title="Clear context"
        message="This clears claude's working memory and re-primes the session. Past messages stay visible but won't be replayed if the chat is later reopened."
        confirmLabel="Clear context"
        variant="default"
        onConfirm={() => void confirmClearContext()}
        onCancel={cancelClear}
      />
      <ConfirmModal
        open={pendingDelete !== null}
        title="Delete chat?"
        message="This removes the session and its transcript permanently."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={() => void confirmDelete()}
        onCancel={cancelDelete}
      />
    </>
  );
}
