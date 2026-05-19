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
   * Session-metadata updates pushed by the runner-log bridge (context_tokens
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
    prevStatusRef.current = undefined;
    clear();
  }

  useEffect(() => {
    if (!sessionID) {
      return;
    }

    let stopped = false;
    let retry = 1000;
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let lastSeq = 0;

    const subscribe = (sinceSeq: number) => {
      if (stopped) return;
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
          const data = JSON.parse((ev as MessageEvent).data) as ChatSessionUpdate;
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
