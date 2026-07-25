import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import type { ChatMessage, ChatSessionUpdate, ChatStatus, LogEntry } from '../types';
import { useRingBuffer } from './useRingBuffer';
import { notifyChatSessionsChanged } from './useChatSessions';

interface ChatSSEEvent {
  seq: number;
  role: string;
  content: string;
  /**
   * Structural marker, e.g. "divider" for the Clear Context sentinel.
   * Empty / absent means a regular message. Matched on by ChatPanel to
   * render a horizontal rule instead of the normal system-message style.
   */
  kind?: string;
  rehydration_phase?: boolean;
}

function roleToType(role: string): LogEntry['type'] {
  switch (role) {
    case 'user':
      return 'user';
    case 'assistant_text':
      return 'text';
    case 'assistant_thinking':
      return 'thinking';
    case 'tool_call':
      return 'tool_call';
    case 'tool_result':
    case 'tool_result_summary':
      return 'tool_result';
    case 'stderr':
      return 'stderr';
    case 'system':
      return 'system';
    default:
      return 'text';
  }
}

function eventToLog(e: ChatSSEEvent): LogEntry {
  return {
    ts: new Date().toISOString(),
    card_id: '',
    type: roleToType(e.role),
    content: e.content,
    seq: e.seq,
    kind: e.kind,
    rehydration_phase: e.rehydration_phase,
  };
}

function messageToLog(m: ChatMessage): LogEntry {
  return {
    ts: m.created_at,
    card_id: '',
    type: roleToType(m.role),
    content: m.content,
    seq: m.seq,
    kind: m.kind,
    rehydration_phase: m.rehydration_phase,
  };
}

export interface UseChatStream {
  logs: readonly LogEntry[];
  connected: boolean;
  /**
   * Session-metadata updates pushed by the worker-log bridge (context_tokens
   * increments after each Claude turn, rehydration_active flips when the
   * agent calls chat_rehydration_complete, model on first usage event).
   * `null` until the first session_updated event arrives.
   */
  sessionUpdate: ChatSessionUpdate | null;
  /** True when older persisted history exists below the loaded window. */
  hasMore: boolean;
  /** True while a loadOlder() page fetch is in flight. */
  loadingOlder: boolean;
  /** Fetches the next OLDER_PAGE_LIMIT messages before the oldest loaded seq
   *  and prepends them. No-op while loading, when hasMore is false, or
   *  before the bootstrap resolves. */
  loadOlder: () => Promise<void>;
}

const CHAT_LOG_RING_CAPACITY = 2000;
/** Newest-first bootstrap page. Small enough to load instantly, big enough
 *  that the rendering layer's tail window has local scroll-back data. */
const BOOTSTRAP_TAIL_LIMIT = 200;
/** Rows fetched per loadOlder() page. */
const OLDER_PAGE_LIMIT = 200;

/**
 * Bootstraps the chat transcript from SQLite then subscribes to the SSE
 * stream. The REST bootstrap loads the NEWEST page of persisted history (so
 * a refresh restores the recent transcript instantly even for huge
 * sessions); the SSE subscription picks up new events from the bootstrap's
 * last seq onward, and loadOlder() pages backward on demand. Replay overlap
 * is deduped by seq, so every seam is gapless without doubling messages.
 * hasMore derives from the seq-contiguity invariant (seqs run 1..N with no
 * holes): history remains below the window exactly while oldest seq > 1.
 */
export function useChatStream(sessionID: string): UseChatStream {
  const { logs, append, prepend, clear } = useRingBuffer(CHAT_LOG_RING_CAPACITY);
  const [connected, setConnected] = useState(false);
  const [sessionUpdate, setSessionUpdate] = useState<ChatSessionUpdate | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  // prevStatusRef tracks the last-seen status value so the comparison runs
  // once per real SSE event (not twice under StrictMode setter double-invoke).
  const prevStatusRef = useRef<ChatStatus | undefined>(undefined);
  /** Oldest loaded seq; null until a non-empty bootstrap page lands. */
  const oldestSeqRef = useRef<number | null>(null);
  /** Synchronous re-entrancy guard - state updates are async. */
  const loadingOlderRef = useRef(false);
  /** Stream epoch, bumped by the main effect on every (re)subscription. An
   *  in-flight loadOlder captures it and discards its result on mismatch.
   *  An epoch (not the session ID) is required: an A→B→A pane swap restores
   *  the same ID, and a page fetched for the FIRST A lifetime landing in the
   *  re-bootstrapped second one would corrupt oldestSeqRef and duplicate
   *  rows. */
  const streamEpochRef = useRef(0);

  const [prevSessionID, setPrevSessionID] = useState(sessionID);
  if (sessionID !== prevSessionID) {
    setPrevSessionID(sessionID);
    setConnected(false);
    setSessionUpdate(null);
    setHasMore(false);
    setLoadingOlder(false);
    clear();
  }

  useEffect(() => {
    if (!sessionID) {
      return;
    }

    // Reset the per-session trackers for the new session. Done here (not in
    // the in-render state-marker block above) so the writes happen outside
    // render - the react-hooks/refs lint rule forbids ref writes during
    // render. The previous effect's cleanup has already closed the old
    // EventSource by this point, so no handler from the old session can
    // race the reset.
    prevStatusRef.current = undefined;
    oldestSeqRef.current = null;
    loadingOlderRef.current = false;
    streamEpochRef.current += 1;

    let stopped = false;
    let retry = 1000;
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let lastSeq = 0;

    const VALID_CHAT_STATUSES = new Set<string>(['cold', 'active', 'warm-idle', 'ending']);
    function isChatStatus(v: unknown): v is ChatStatus {
      return typeof v === 'string' && VALID_CHAT_STATUSES.has(v);
    }

    const subscribe = (sinceSeq: number) => {
      if (stopped) return;
      // Record the seq at the start of this connection window so that
      // onerror can detect whether real progress was made and reset backoff.
      const seqAtConnectStart = lastSeq;
      es = new EventSource(`/api/chats/${encodeURIComponent(sessionID)}/stream?since_seq=${sinceSeq}`);
      es.onopen = () => {
        setConnected(true);
        retry = 1000;
      };
      es.onmessage = (ev) => {
        try {
          const data = JSON.parse(ev.data) as ChatSSEEvent;
          if (typeof data.seq === 'number') {
            if (data.seq <= lastSeq) return;
            lastSeq = data.seq;
          }
          append([eventToLog(data)]);
        } catch {
          // malformed payload - skip
        }
      };
      es.addEventListener('session_updated', (ev) => {
        try {
          const raw = JSON.parse((ev as MessageEvent).data) as Record<string, unknown>;
          // Narrow the status field before use - a malformed value from the
          // server must not pollute React state with an unexpected string.
          const data: ChatSessionUpdate = {
            ...(typeof raw.context_tokens === 'number' && { context_tokens: raw.context_tokens }),
            ...(typeof raw.context_tokens_updated_at === 'string' && {
              context_tokens_updated_at: raw.context_tokens_updated_at,
            }),
            ...(typeof raw.model === 'string' && { model: raw.model }),
            ...(typeof raw.rehydration_active === 'boolean' && {
              rehydration_active: raw.rehydration_active,
            }),
            ...(isChatStatus(raw.status) && { status: raw.status }),
            ...(typeof raw.estimated_cost_usd === 'number' && { estimated_cost_usd: raw.estimated_cost_usd }),
            ...(typeof raw.prompt_tokens === 'number' && { prompt_tokens: raw.prompt_tokens }),
            ...(typeof raw.completion_tokens === 'number' && { completion_tokens: raw.completion_tokens }),
            ...(typeof raw.cache_read_tokens === 'number' && { cache_read_tokens: raw.cache_read_tokens }),
            ...(typeof raw.cache_creation_tokens === 'number' && { cache_creation_tokens: raw.cache_creation_tokens }),
            ...(typeof raw.assistant_working === 'boolean' && {
              assistant_working: raw.assistant_working,
            }),
            ...(typeof raw.assistant_working_since === 'string' && {
              assistant_working_since: raw.assistant_working_since,
            }),
          };
          // Compare status once per real event using a ref - avoids the
          // double-dispatch that would occur if the comparison lived inside
          // the setSessionUpdate setter (which StrictMode invokes twice).
          if (data.status !== undefined && data.status !== prevStatusRef.current) {
            prevStatusRef.current = data.status;
            notifyChatSessionsChanged();
          }
          setSessionUpdate((prev) => ({ ...(prev ?? {}), ...data }));
        } catch {
          // malformed payload - skip
        }
      });
      es.onerror = () => {
        setConnected(false);
        es?.close();
        es = null;
        if (stopped) return;
        // Reset backoff when real progress was made during this connection
        // window (i.e. at least one new message arrived). This prevents the
        // backoff from compounding across transient mid-stream disconnects.
        if (lastSeq > seqAtConnectStart) {
          retry = 1000;
        }
        retryTimer = setTimeout(() => subscribe(lastSeq), retry);
        retry = Math.min(retry * 2, 30000);
      };
    };

    (async () => {
      try {
        const result = await api.listChatMessagesTail(sessionID, BOOTSTRAP_TAIL_LIMIT);
        if (stopped) return;
        if (result.messages.length > 0) {
          const entries = result.messages.map(messageToLog);
          append(entries);
          lastSeq = result.messages[result.messages.length - 1].seq;
          oldestSeqRef.current = result.messages[0].seq;
          setHasMore(result.messages[0].seq > 1);
        }
      } catch {
        // Bootstrap failed - fall back to SSE-only (the hub ring replays the
        // newest events from since_seq=0). hasMore stays false.
      }
      // Guard against the effect being torn down while the bootstrap await
      // was in flight (e.g. React StrictMode double-invoke, or the user
      // navigated away). Without this check the cleanup's `stopped = true`
      // would be ignored and a stale EventSource would be opened.
      if (stopped) return;
      subscribe(lastSeq);
    })();

    return () => {
      stopped = true;
      if (retryTimer) clearTimeout(retryTimer);
      es?.close();
    };
  }, [sessionID, append]);

  const loadOlder = useCallback(async () => {
    const before = oldestSeqRef.current;
    if (before === null || before <= 1) return; // nothing loaded yet / at seq 1
    if (loadingOlderRef.current) return; // serialize: one page in flight

    const epoch = streamEpochRef.current;
    loadingOlderRef.current = true;
    setLoadingOlder(true);
    try {
      const result = await api.listChatMessagesBefore(sessionID, before, OLDER_PAGE_LIMIT);
      if (streamEpochRef.current !== epoch) return; // stream re-bootstrapped mid-flight
      const msgs = result.messages;
      if (msgs.length === 0) {
        // Defensive: contiguity means this only happens if rows vanished.
        setHasMore(false);
        return;
      }
      const inserted = prepend(msgs.map(messageToLog));
      if (inserted === 0) {
        // Ring full - history beyond the in-memory cap stays server-side.
        setHasMore(false);
        return;
      }
      // prepend keeps the NEWEST `inserted` entries of the batch, so the
      // first surviving row is msgs[msgs.length - inserted].
      const newOldest = msgs[msgs.length - inserted].seq;
      oldestSeqRef.current = newOldest;
      setHasMore(inserted === msgs.length && newOldest > 1);
    } catch {
      // Transient fetch failure - keep hasMore so the user can retry.
    } finally {
      // A stale call must not touch the guards: the effect already reset
      // them for the new epoch, and a fresh fetch may be in flight - an
      // unconditional clear here would unlock a concurrent duplicate page.
      if (streamEpochRef.current === epoch) {
        loadingOlderRef.current = false;
        setLoadingOlder(false);
      }
    }
  }, [sessionID, prepend]);

  return { logs, connected, sessionUpdate, hasMore, loadingOlder, loadOlder };
}
