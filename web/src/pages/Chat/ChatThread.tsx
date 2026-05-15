import { useEffect, useMemo, useRef, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { ChatSession } from '../../types';
import { ChatPanel } from '../../components/ChatPanel';
import { useChatStream } from '../../hooks/useChatStream';
import { CHAT_SESSIONS_CHANGED_EVENT, notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { clearChatLiveData, setChatLiveData } from '../../hooks/useChatLiveData';
import { ConfirmModal } from '../../components/ConfirmModal/ConfirmModal';
import { ChatHeaderInfo } from './ChatHeaderInfo';

interface ChatThreadProps {
  sessionID: string;
  onDeleted?: () => void;
  // When true, suppresses the internal header (title/model/status + End/Reopen/
  // Delete buttons) and the delete confirmation modal. Used inside the
  // multi-pane layout where the wrapping pane provides its own header and
  // useChatLayout owns the focused-pane last_chat_id write.
  embedded?: boolean;
  // Pane focus signal. When embedded and focused, the compose textarea
  // grabs focus on mount + whenever this becomes true. Defaults to true for
  // non-embedded usage so single-pane behavior is unchanged.
  isFocused?: boolean;
}

export function ChatThread({ sessionID, onDeleted, embedded = false, isFocused = true }: ChatThreadProps) {
  const [session, setSession] = useState<ChatSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [ending, setEnding] = useState(false);
  const [reopening, setReopening] = useState(false);
  const { logs, connected, sessionUpdate } = useChatStream(sessionID);

  // Merge live session updates (context_tokens, model, rehydration_active)
  // from the SSE wire on top of the snapshot returned by GET /api/chats/{id}.
  // This avoids triggering a setState inside the SSE handler (which would
  // race against unmount) and keeps the source-of-truth in one place.
  const merged = useMemo(() => {
    if (!session) return null;
    if (!sessionUpdate) return session;
    return {
      ...session,
      ...(sessionUpdate.context_tokens !== undefined
        ? { context_tokens: sessionUpdate.context_tokens }
        : {}),
      ...(sessionUpdate.context_tokens_updated_at !== undefined
        ? { context_tokens_updated_at: sessionUpdate.context_tokens_updated_at }
        : {}),
      ...(sessionUpdate.model !== undefined ? { model: sessionUpdate.model } : {}),
      ...(sessionUpdate.rehydration_active !== undefined
        ? { rehydration_active: sessionUpdate.rehydration_active }
        : {}),
    } as ChatSession;
  }, [session, sessionUpdate]);

  // Reset local state synchronously when the sessionID prop changes — see
  // web/CLAUDE.md § CardPanel for why this lives in render, not useEffect.
  const [prevSessionID, setPrevSessionID] = useState(sessionID);
  if (sessionID !== prevSessionID) {
    setPrevSessionID(sessionID);
    setSession(null);
    setError(null);
    setConfirmDelete(false);
    setEnding(false);
    setReopening(false);
  }

  useEffect(() => {
    let alive = true;
    api
      .getChat(sessionID)
      .then((s) => {
        if (alive) setSession(s);
      })
      .catch((e) => {
        if (alive) setError(isAPIError(e) ? e.error : 'failed to load chat');
      });
    return () => {
      alive = false;
    };
  }, [sessionID]);

  // Re-fetch the session whenever the sidebar's chat list refreshes (which
  // fires after any external state-changing action: Reopen / End / Delete
  // from another pane, NewChatDialog, etc). Without this, the local
  // `session.status` stays at whatever GetChat returned on mount — so e.g.
  // clicking "Reopen" from the pane header would flip the backend to
  // active but the compose box would keep showing "Waiting for runner…".
  useEffect(() => {
    if (typeof window === 'undefined') return;
    let alive = true;
    const handler = () => {
      api.getChat(sessionID)
        .then((s) => { if (alive) setSession(s); })
        .catch(() => { /* transient — next event will retry */ });
    };
    window.addEventListener(CHAT_SESSIONS_CHANGED_EVENT, handler);
    return () => {
      alive = false;
      window.removeEventListener(CHAT_SESSIONS_CHANGED_EVENT, handler);
    };
  }, [sessionID]);

  // Remember the active chat so /chat can auto-restore it after a refresh
  // or a fresh sidebar navigation. Cleared from handleDelete when the user
  // removes this chat so we don't keep redirecting to a 404. Skipped when
  // embedded: useChatLayout writes last_chat_id for the focused pane.
  useEffect(() => {
    if (embedded) return;
    try {
      window.localStorage.setItem('last_chat_id', sessionID);
    } catch {
      // Storage disabled (privacy mode, quota) — non-fatal.
    }
  }, [sessionID, embedded]);

  // Refresh the sidebar's session list whenever this chat's status changes
  // (cold → active after Reopen, active → ending after End, etc). Backend
  // doesn't broadcast status updates over SSE, so each ChatThread is the
  // first to learn — propagate via the existing notify event.
  const lastNotifiedStatusRef = useRef<ChatSession['status'] | undefined>(undefined);
  useEffect(() => {
    const status = merged?.status;
    if (status && status !== lastNotifiedStatusRef.current) {
      lastNotifiedStatusRef.current = status;
      notifyChatSessionsChanged();
    }
  }, [merged?.status]);

  // Publish live model + context_tokens into the module-level store so the
  // wrapping PaneHeader (a sibling above this component) can show the
  // context-window % without prop-drilling through ChatLayout. Cleared on
  // unmount + when sessionID changes so stale data for closed panes is GC'd.
  useEffect(() => {
    if (!merged) return;
    setChatLiveData(sessionID, {
      model: merged.model,
      contextTokens: merged.context_tokens,
      contextTokensUpdatedAt: merged.context_tokens_updated_at,
    });
  }, [sessionID, merged]);
  useEffect(() => {
    return () => clearChatLiveData(sessionID);
  }, [sessionID]);

  // Suppress the "Restoring workspace…" banner once the user has successfully
  // sent at least one message in this session. If they're chatting, the runner
  // is clearly ready — the rehydration_active flag can stick true forever when
  // the agent forgets to call chat_rehydration_complete (per the
  // sweepStaleRehydration comment in internal/chat/reaper.go). This is a UX
  // workaround for that backend signaling gap; the reaper still clears the
  // server-side flag eventually.
  const [userHasSent, setUserHasSent] = useState(false);
  const [prevSendSession, setPrevSendSession] = useState(sessionID);
  if (sessionID !== prevSendSession) {
    setPrevSendSession(sessionID);
    setUserHasSent(false);
  }

  if (error) {
    return (
      <div className="p-4 text-sm" style={{ color: 'var(--red)' }}>{error}</div>
    );
  }
  if (!merged) {
    return (
      <div className="p-4 text-sm" style={{ color: 'var(--grey1)' }}>Loading…</div>
    );
  }
  const view = merged;

  const handleSend = async (content: string) => {
    try {
      await api.sendChatMessage(sessionID, content);
      setUserHasSent(true);
    } catch (e) {
      const msg = isAPIError(e) ? e.error : 'Failed to send message';
      throw new Error(msg, { cause: e });
    }
  };

  const handleEnd = async () => {
    setEnding(true);
    try {
      const fresh = await api.endChat(sessionID);
      setSession(fresh);
    } catch (e) {
      setError(isAPIError(e) ? e.error : 'Failed to end session');
    } finally {
      setEnding(false);
    }
  };

  const handleReopen = async () => {
    setReopening(true);
    try {
      const fresh = await api.openChat(sessionID);
      setSession(fresh);
    } catch (e) {
      setError(isAPIError(e) ? e.error : 'Failed to reopen session');
    } finally {
      setReopening(false);
    }
  };

  const handleDelete = async () => {
    setConfirmDelete(false);
    try {
      await api.deleteChat(sessionID);
      try {
        window.localStorage.removeItem('last_chat_id');
      } catch {
        // Storage disabled — non-fatal.
      }
      notifyChatSessionsChanged();
      onDeleted?.();
    } catch (e) {
      setError(isAPIError(e) ? e.error : 'Failed to delete chat');
    }
  };

  const isRunning = view.status === 'active' || view.status === 'warm-idle';
  const isCold = view.status === 'cold';
  const isEnding = view.status === 'ending';

  // While the container is being warmed (cold→active transition driven by
  // openChat from NewChatDialog, or the user clicking Reopen), the textarea
  // is replaced with a banner. Once status flips to 'active' the container
  // is up and reading stdin; claude in `-p --input-format stream-json` mode
  // doesn't emit anything until the first user message, so we cannot wait
  // for an inbound event without deadlocking the user out of sending.
  const readOnlyMessage =
    isEnding ? 'Session is ending…' :
    isCold ? 'Waiting for runner…' :
    undefined;
  const sendDisabled = isEnding || isCold;

  return (
    <div className="flex flex-col h-full min-h-0">
      {!embedded && (
        <div
          className="flex items-center gap-3 px-4 py-2 border-b shrink-0"
          style={{ borderColor: 'var(--bg3)', backgroundColor: 'var(--bg0)' }}
        >
          <h2 className="text-base font-medium flex-1 truncate" style={{ color: 'var(--fg)' }}>
            {view.title || '(untitled)'}
          </h2>
          {view.project && (
            <span
              className="text-xs px-2 py-0.5 rounded font-mono"
              style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}
            >
              {view.project}
            </span>
          )}
          <ChatHeaderInfo model={view.model} contextTokens={view.context_tokens} />
          <span className="text-xs" style={{ color: 'var(--grey1)' }}>{view.status}</span>
          <span
            className="w-2 h-2 rounded-full shrink-0"
            title={connected ? 'Stream connected' : 'Stream disconnected'}
            aria-label={connected ? 'Stream connected' : 'Stream disconnected'}
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--grey1)' }}
          />
          {isRunning && (
            <button
              onClick={() => void handleEnd()}
              disabled={ending}
              className="bf-btn-ghost bf-btn-sm"
              style={{ color: 'var(--orange)', borderColor: 'color-mix(in oklab, var(--orange) 35%, transparent)' }}
            >
              {ending ? 'Ending…' : 'End session'}
            </button>
          )}
          {isCold && (
            <button
              onClick={() => void handleReopen()}
              disabled={reopening}
              className="bf-btn-ghost bf-btn-sm"
              style={{ color: 'var(--green)' }}
            >
              {reopening ? 'Reopening…' : 'Reopen'}
            </button>
          )}
          <button
            onClick={() => setConfirmDelete(true)}
            className="bf-btn-ghost bf-btn-sm"
            style={{ color: 'var(--red)' }}
          >
            Delete
          </button>
        </div>
      )}
      {view.rehydration_active && !userHasSent && (
        <div
          className="px-4 py-2 text-xs border-b shrink-0 flex items-center gap-2"
          style={{
            backgroundColor: 'var(--bg-blue)',
            color: 'var(--aqua)',
            borderColor: 'var(--bg3)',
          }}
        >
          <span
            className="inline-block w-2 h-2 rounded-full animate-pulse"
            style={{ backgroundColor: 'var(--aqua)' }}
            aria-hidden="true"
          />
          <span>
            Restoring workspace from prior session — the agent is re-establishing
            context (cloning repos, restoring branches). You can start typing as
            soon as it announces it's ready.
          </span>
        </div>
      )}
      <div className="flex-1 min-h-0">
        <ChatPanel
          logs={logs}
          onSend={handleSend}
          sendDisabled={sendDisabled}
          readOnlyMessage={readOnlyMessage}
          focusKey={isFocused ? sessionID : undefined}
        />
      </div>
      {!embedded && (
        <ConfirmModal
          open={confirmDelete}
          title="Delete chat?"
          message="This removes the session and its transcript permanently."
          confirmLabel="Delete"
          variant="danger"
          onConfirm={() => void handleDelete()}
          onCancel={() => setConfirmDelete(false)}
        />
      )}
    </div>
  );
}
