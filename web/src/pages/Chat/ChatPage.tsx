import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { NewChatDialog } from './NewChatDialog';
import { ChatThread } from './ChatThread';
import { MobileChatHeader } from './MobileChatHeader';
import { ChatLayout, ChatLayoutProvider, type AvailableChat, type Slot } from '../../components/ChatLayout';
import { useChatLayout, type ChatLayoutState, type LRUEvictionEvent } from '../../hooks/useChatLayout';
import { useChatSessions, notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { api, isAPIError } from '../../api/client';
import { ConfirmModal } from '../../components/ConfirmModal/ConfirmModal';
import {
  CHAT_DRAG_START_EVENT,
  CHAT_DRAG_END_EVENT,
} from '../../components/Sidebar/ChatSection';

interface EvictToast {
  victimChatId: string;
  incomingChatId: string;
  snapshot: ChatLayoutState;
}

export function ChatPage() {
  const { id: deepLinkId } = useParams();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const wantsNew = params.get('new') === '1';
  const [dialogOpen, setDialogOpen] = useState(wantsNew);

  const { sessions } = useChatSessions();
  const isDesktop = useMediaQuery('(min-width: 768px)');
  const isMobile = !isDesktop;

  const availableChats = useMemo<AvailableChat[]>(
    () => sessions.map((s) => ({
      id: s.id,
      title: s.title || '(untitled)',
      status: s.status,
      model: s.model,
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
  // overlay can show the chat name. We use events rather than a shared
  // context because ChatSection lives outside ChatLayoutProvider's subtree.
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

  // /chat/:id deep links: open the chat as a new pane on top of the
  // hydrated layout, then bounce back to /chat so refresh doesn't re-fire.
  // In-render marker (CardPanel idiom) — not useEffect — so the redirect
  // is synchronous with the prop change and avoids a double render.
  const [prevDeepLinkId, setPrevDeepLinkId] = useState<string | undefined>(deepLinkId);
  if (deepLinkId && deepLinkId !== prevDeepLinkId) {
    setPrevDeepLinkId(deepLinkId);
    layout.openInNewPane(deepLinkId);
    navigate('/chat', { replace: true });
  } else if (!deepLinkId && prevDeepLinkId) {
    setPrevDeepLinkId(undefined);
  }

  // ?new=1 → open NewChatDialog (matches the prior in-render reset pattern)
  const [prevWantsNew, setPrevWantsNew] = useState(wantsNew);
  if (wantsNew !== prevWantsNew) {
    setPrevWantsNew(wantsNew);
    if (wantsNew) setDialogOpen(true);
  }

  const closeDialog = () => {
    setDialogOpen(false);
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

  const renderPaneBody = useCallback(
    (chatId: string, _slot: Slot, isFocused: boolean) => (
      <ChatThread sessionID={chatId} embedded isFocused={isFocused} />
    ),
    [],
  );

  // Per-pane chat actions (End / Reopen / Delete). Each calls the API,
  // notifies the sidebar so the status dot reflects the new state, and
  // for Delete also closes the pane that hosted the chat.
  const handleEndSession = useCallback(async (chatId: string) => {
    try {
      await api.endChat(chatId);
    } catch (e) {
      console.warn('endChat failed', isAPIError(e) ? e.error : e);
    } finally {
      notifyChatSessionsChanged();
    }
  }, []);

  const handleReopenChat = useCallback(async (chatId: string) => {
    try {
      await api.openChat(chatId);
    } catch (e) {
      console.warn('openChat failed', isAPIError(e) ? e.error : e);
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
      console.warn('clearChatContext failed', isAPIError(e) ? e.error : e);
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
      try { window.localStorage.removeItem('last_chat_id'); } catch { /* ignore */ }
    } catch (e) {
      console.warn('deleteChat failed', isAPIError(e) ? e.error : e);
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
    <ChatLayoutProvider value={layout}>
      <div className="flex flex-col h-full">
        {isMobile && (
          <MobileChatHeader
            title={mobileTitle}
            onNewChat={() => setDialogOpen(true)}
          />
        )}
        <div className="flex-1 min-h-0">
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
            onPickEmptyPane={layout.swapPaneChat}
            onCancelEmpty={layout.cancelEmptyPane}
            onNewChat={() => setDialogOpen(true)}
            onEndSession={handleEndSession}
            onClearContext={requestClearContext}
            onReopenChat={handleReopenChat}
            onDeleteChat={requestDeleteChat}
            renderPaneBody={renderPaneBody}
          />
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
      <NewChatDialog open={dialogOpen} onClose={closeDialog} />
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
    </ChatLayoutProvider>
  );
}
