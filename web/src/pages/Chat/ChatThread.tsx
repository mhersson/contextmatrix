import { useEffect, useMemo, useRef, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { ChatSession } from '../../types';
import { ChatPanel } from '../../components/ChatPanel';
import { useChatStream } from '../../hooks/useChatStream';
import { useWorkingState } from '../../hooks/useWorkingState';
import { CHAT_SESSIONS_CHANGED_EVENT, notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { clearChatLiveData, setChatLiveData } from '../../hooks/useChatLiveData';

interface ChatThreadProps {
  sessionID: string;
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

export function ChatThread({ sessionID, embedded = false, isFocused = true }: ChatThreadProps) {
  const [session, setSession] = useState<ChatSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { logs, sessionUpdate } = useChatStream(sessionID);

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
      ...(sessionUpdate.estimated_cost_usd !== undefined
        ? { estimated_cost_usd: sessionUpdate.estimated_cost_usd }
        : {}),
      ...(sessionUpdate.prompt_tokens !== undefined
        ? { prompt_tokens: sessionUpdate.prompt_tokens }
        : {}),
      ...(sessionUpdate.completion_tokens !== undefined
        ? { completion_tokens: sessionUpdate.completion_tokens }
        : {}),
      ...(sessionUpdate.cache_read_tokens !== undefined
        ? { cache_read_tokens: sessionUpdate.cache_read_tokens }
        : {}),
      ...(sessionUpdate.cache_creation_tokens !== undefined
        ? { cache_creation_tokens: sessionUpdate.cache_creation_tokens }
        : {}),
      ...(sessionUpdate.assistant_working !== undefined
        ? { assistant_working: sessionUpdate.assistant_working }
        : {}),
      ...(sessionUpdate.assistant_working_since !== undefined
        ? { assistant_working_since: sessionUpdate.assistant_working_since }
        : {}),
    } as ChatSession;
  }, [session, sessionUpdate]);

  const { working, armOptimistic } = useWorkingState(sessionID, merged);

  // Reset local state synchronously when the sessionID prop changes - see
  // web/CLAUDE.md § CardPanel for why this lives in render, not useEffect.
  const [prevSessionID, setPrevSessionID] = useState(sessionID);
  if (sessionID !== prevSessionID) {
    setPrevSessionID(sessionID);
    setSession(null);
    setError(null);
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
  // `session.status` stays at whatever GetChat returned on mount - so e.g.
  // clicking "Reopen" from the pane header would flip the backend to
  // active but the compose box would keep showing "Waiting for worker…".
  //
  // Debounced at 75ms: useChatSessions already coalesces up to 4 pane
  // notifications into one sidebar refetch (100ms window), but each pane's
  // own listener here was also firing immediately. The debounce cuts fan-out
  // from 4 redundant getChat calls down to 1 per burst across all panes.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    let alive = true;
    let debounceTimer: ReturnType<typeof setTimeout> | null = null;
    const handler = () => {
      if (debounceTimer !== null) return;
      debounceTimer = setTimeout(() => {
        debounceTimer = null;
        if (!alive) return;
        api.getChat(sessionID)
          .then((s) => { if (alive) setSession(s); })
          .catch(() => { /* transient - next event will retry */ });
      }, 75);
    };
    window.addEventListener(CHAT_SESSIONS_CHANGED_EVENT, handler);
    return () => {
      alive = false;
      if (debounceTimer !== null) clearTimeout(debounceTimer);
      window.removeEventListener(CHAT_SESSIONS_CHANGED_EVENT, handler);
    };
  }, [sessionID]);

  // Remember the active chat so /chat can auto-restore it after a refresh
  // or a fresh sidebar navigation. Skipped when embedded: useChatLayout
  // writes last_chat_id for the focused pane.
  useEffect(() => {
    if (embedded) return;
    try {
      window.localStorage.setItem('last_chat_id', sessionID);
    } catch {
      // Storage disabled (privacy mode, quota) - non-fatal.
    }
  }, [sessionID, embedded]);

  // Refresh the sidebar's session list whenever this chat's status changes
  // (cold → active after Reopen, active → ending after End, etc). Backend
  // doesn't broadcast status updates over SSE, so each ChatThread is the
  // first to learn - propagate via the existing notify event.
  //
  // Sentinel: undefined means "not yet seen". The first time merged resolves
  // we record the status silently (no notification) - this avoids a
  // spurious refetch on every ChatThread mount.
  const lastNotifiedStatusRef = useRef<ChatSession['status'] | undefined>(undefined);
  useEffect(() => {
    const status = merged?.status;
    if (status === undefined) return;
    if (lastNotifiedStatusRef.current === undefined) {
      // First observation: seed the ref so subsequent real changes fire.
      lastNotifiedStatusRef.current = status;
      return;
    }
    if (status === lastNotifiedStatusRef.current) return;
    lastNotifiedStatusRef.current = status;
    notifyChatSessionsChanged();
  }, [merged?.status]);

  // Publish live model + context_tokens + cost fields into the module-level
  // store so the wrapping PaneHeader (a sibling above this component) can show
  // the context-window % and running cost without prop-drilling through
  // ChatLayout. Cleared on unmount + when sessionID changes so stale data for
  // closed panes is GC'd.
  // The effect re-runs on every `merged` recompute; the store's `shallowEqualLive` dedups identical writes.
  useEffect(() => {
    if (!merged) return;
    setChatLiveData(sessionID, {
      model: merged.model,
      contextTokens: merged.context_tokens,
      contextTokensUpdatedAt: merged.context_tokens_updated_at,
      estimatedCostUsd: merged.estimated_cost_usd,
      promptTokens: merged.prompt_tokens,
      completionTokens: merged.completion_tokens,
      cacheReadTokens: merged.cache_read_tokens,
      cacheCreationTokens: merged.cache_creation_tokens,
    });
  }, [sessionID, merged]);
  useEffect(() => {
    return () => clearChatLiveData(sessionID);
  }, [sessionID]);

  // Suppress the "Restoring workspace…" banner once the user has successfully
  // sent at least one message in this session. If they're chatting, the worker
  // is clearly ready - the rehydration_active flag can stick true forever when
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
      armOptimistic();
    } catch (e) {
      const msg = isAPIError(e) ? e.error : 'Failed to send message';
      throw new Error(msg, { cause: e });
    }
  };

  const isEnding = view.status === 'ending';
  const isCold = view.status === 'cold';

  // While the container is being warmed (cold→active transition driven by
  // openChat from NewChatDialog, or the user clicking Reopen), the textarea
  // is replaced with a banner. Once status flips to 'active' the container
  // is up and reading stdin; claude in `-p --input-format stream-json` mode
  // doesn't emit anything until the first user message, so we cannot wait
  // for an inbound event without deadlocking the user out of sending.
  const readOnlyMessage =
    isEnding ? 'Session is ending…' :
    isCold ? 'Waiting for worker…' :
    undefined;
  const sendDisabled = isEnding || isCold;

  return (
    <div className="flex flex-col h-full min-h-0">
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
            Restoring workspace from prior session - the agent is re-establishing
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
          working={working}
        />
      </div>
    </div>
  );
}
