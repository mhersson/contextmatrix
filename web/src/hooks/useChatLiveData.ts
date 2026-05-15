import { useSyncExternalStore } from 'react';

export interface ChatLiveData {
  contextTokens?: number;
  contextTokensUpdatedAt?: string;
  model?: string;
}

// Module-level store. Acceptable for a small read-by-PaneHeader / written-by-
// ChatThread signal that doesn't fit cleanly inside useChatLayout (which owns
// layout state). Avoids prop-drilling live transcript metadata through
// ChatLayout → ChatPane → PaneHeader.
const store: Map<string, ChatLiveData> = new Map();
const subscribers: Set<() => void> = new Set();

function notify(): void {
  subscribers.forEach((s) => s());
}

function shallowEqualLive(a: ChatLiveData | undefined, b: ChatLiveData): boolean {
  if (!a) return false;
  return (
    a.contextTokens === b.contextTokens &&
    a.contextTokensUpdatedAt === b.contextTokensUpdatedAt &&
    a.model === b.model
  );
}

export function setChatLiveData(chatId: string, partial: ChatLiveData): void {
  const prev = store.get(chatId);
  const next: ChatLiveData = { ...(prev ?? {}), ...partial };
  if (shallowEqualLive(prev, next)) return;
  store.set(chatId, next);
  notify();
}

export function clearChatLiveData(chatId: string): void {
  if (!store.has(chatId)) return;
  store.delete(chatId);
  notify();
}

function subscribe(callback: () => void): () => void {
  subscribers.add(callback);
  return () => {
    subscribers.delete(callback);
  };
}

export function useChatLiveData(chatId: string | null | undefined): ChatLiveData | undefined {
  return useSyncExternalStore(
    subscribe,
    () => (chatId ? store.get(chatId) : undefined),
    () => undefined,
  );
}
