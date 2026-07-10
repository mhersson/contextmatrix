import { useEffect, useRef, useState } from 'react';
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
}

const CHAT_LOG_RING_CAPACITY = 2000;
const BOOTSTRAP_LIMIT = 1000;

/**
 * Bootstraps the chat transcript from SQLite then subscribes to the SSE
 * stream. The REST bootstrap fills the in-browser ring buffer with persisted
 * history (so a refresh restores the transcript); the SSE subscription picks
 * up new events from the bootstrap's last seq onward. Replay overlap is
 * deduped by seq, so the seam is gapless without doubling messages.
 */
export function useChatStream(sessionID: string): UseChatStream {
  const { logs, append, clear } = useRingBuffer(CHAT_LOG_RING_CAPACITY);
  const [connected, setConnected] = useState(false);
  const [sessionUpdate, setSessionUpdate] = useState<ChatSessionUpdate | null>(null);
  // prevStatusRef tracks the last-seen status value so the comparison runs
  // once per real SSE event (not twice under StrictMode setter double-invoke).
  const prevStatusRef = useRef<ChatStatus | undefined>(undefined);

  const [prevSessionID, setPrevSessionID] = useState(sessionID);
  if (sessionID !== prevSessionID) {
    setPrevSessionID(sessionID);
    setConnected(false);
    setSessionUpdate(null);
    clear();
  }

  useEffect(() => {
    if (!sessionID) {
      return;
    }

    // Reset the status tracker for the new session. Done here (not in the
    // in-render state-marker block above) so the write happens outside
    // render — the react-hooks/refs lint rule forbids ref writes during
    // render. The previous effect's cleanup has already closed the old
    // EventSource by this point, so no handler from the old session can
    // race the reset.
    prevStatusRef.current = undefined;

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
          // malformed payload — skip
        }
      };
      es.addEventListener('session_updated', (ev) => {
        try {
          const raw = JSON.parse((ev as MessageEvent).data) as Record<string, unknown>;
          // Narrow the status field before use — a malformed value from the
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
          };
          // Compare status once per real event using a ref — avoids the
          // double-dispatch that would occur if the comparison lived inside
          // the setSessionUpdate setter (which StrictMode invokes twice).
          if (data.status !== undefined && data.status !== prevStatusRef.current) {
            prevStatusRef.current = data.status;
            notifyChatSessionsChanged();
          }
          setSessionUpdate((prev) => ({ ...(prev ?? {}), ...data }));
        } catch {
          // malformed payload — skip
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
        const result = await api.listChatMessages(sessionID, 0, BOOTSTRAP_LIMIT);
        if (stopped) return;
        if (result.messages.length > 0) {
          const entries = result.messages.map(messageToLog);
          append(entries);
          lastSeq = result.messages[result.messages.length - 1].seq;
        }
      } catch {
        // Bootstrap failed — fall back to SSE-only.
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

  return { logs, connected, sessionUpdate };
}
