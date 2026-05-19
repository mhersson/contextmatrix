import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { ChatSession } from '../types';

export interface UseChatSessions {
  sessions: ChatSession[];
  refresh: () => void;
  error: string | null;
}

/**
 * Custom DOM event dispatched by mutation sites (NewChatDialog on success,
 * ChatThread on delete) so any mounted useChatSessions instance refreshes
 * its list without prop-drilling a refresh callback through the component
 * tree. Listened for in the hook below.
 */
export const CHAT_SESSIONS_CHANGED_EVENT = 'cm:chat-sessions-changed';

/** Notify all useChatSessions consumers that the chat list changed. */
export function notifyChatSessionsChanged() {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(new Event(CHAT_SESSIONS_CHANGED_EVENT));
}

/**
 * Loads chat sessions from /api/chats on mount and re-fetches whenever
 * any mounted component fires CHAT_SESSIONS_CHANGED_EVENT (via
 * notifyChatSessionsChanged). Also exposes a manual `refresh()` for cases
 * where a component already owns the hook instance.
 *
 * The event listener debounces N rapid-fire notifications (e.g. up to 4 panes
 * each calling notifyChatSessionsChanged on the same status transition) into a
 * single refetch within a 100 ms window.
 */
export function useChatSessions(): UseChatSessions {
  const [sessions, setSessions] = useState<ChatSession[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let alive = true;
    api
      .listChats({})
      .then((data) => {
        if (alive) {
          setSessions(data ?? []);
          setError(null);
        }
      })
      .catch((e) => {
        if (alive) {
          setError(e instanceof Error ? e.message : 'failed to load chats');
        }
      });
    return () => {
      alive = false;
    };
  }, [tick]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    let debounceTimer: ReturnType<typeof setTimeout> | null = null;
    const handler = () => {
      if (debounceTimer !== null) return; // already pending — coalesce
      debounceTimer = setTimeout(() => {
        debounceTimer = null;
        setTick((n) => n + 1);
      }, 100);
    };
    window.addEventListener(CHAT_SESSIONS_CHANGED_EVENT, handler);
    return () => {
      window.removeEventListener(CHAT_SESSIONS_CHANGED_EVENT, handler);
      if (debounceTimer !== null) clearTimeout(debounceTimer);
    };
  }, []);

  return {
    sessions,
    refresh: () => setTick((n) => n + 1),
    error,
  };
}
