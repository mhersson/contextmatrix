import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useSSEBus } from '../hooks/useSSEBus';
import type { ChatModel, ChatModelList } from '../types';

export type ChatModelSource = ChatModelList['source'];

export interface ChatModelsResult {
  models: ChatModel[];
  // source mirrors GET /api/chats/models: 'openrouter' (dedicated chat backend
  // → CM's vendor-screened catalog; id/label are the OpenRouter slug and
  // max_tokens is the context window - consumers no longer fetch the OpenRouter
  // API directly from the browser), or 'endpoint' (server-provided list from
  // the configured OpenAI-compatible endpoint, rendered as a <select>), or
  // 'claude_code' (the Claude Code catalog, rendered the same way). A
  // fetch failure or a missing source degrades to an empty 'endpoint' list,
  // which renders nothing - new chats fall back to the server default.
  source: ChatModelSource;
  /** GET /api/chats/models architect_default: the model a blueprint with no model of its own runs on. */
  architectDefault?: string;
}

/** Whose list: new chats', or new blueprints' (`?kind=architect`), which can run on another LLM channel mode. */
export type ChatModelsKind = 'chat' | 'architect';

const EMPTY: ChatModelsResult = { models: [], source: 'endpoint', architectDefault: '' };

interface Cache {
  promise?: Promise<ChatModelsResult>;
  result?: ChatModelsResult;
}

// Module-scope cache per kind so a single GET /api/chats/models response is
// reused by every component that needs that list (PaneHeader, etc).
const caches: Record<ChatModelsKind, Cache> = { chat: {}, architect: {} };

/** Forget the module-scope model lists; a fetch still in flight lands in the dropped cache. */
export function resetChatModelsCache() {
  caches.chat = {};
  caches.architect = {};
}

/**
 * Forgets both lists on every LLM channel event, so the next consumer fetches
 * the current mode's catalog. Mounted once, by the always-present Sidebar.
 */
export function useChatModelsChannelReset() {
  const { subscribe } = useSSEBus();
  useEffect(() => subscribe('llm_channel.*', resetChatModelsCache), [subscribe]);
}

function loadChatModels(kind: ChatModelsKind): Promise<ChatModelsResult> {
  const cache = caches[kind];
  if (cache.result) return Promise.resolve(cache.result);
  if (cache.promise) return cache.promise;
  cache.promise = api
    .listChatModels(kind === 'architect' ? 'architect' : undefined)
    .then((resp) => {
      const result: ChatModelsResult = { models: resp.models, source: resp.source ?? 'endpoint',
        architectDefault: resp.architect_default ?? '',
      };
      cache.result = result;
      return result;
    })
    .catch((): ChatModelsResult => EMPTY);
  return cache.promise;
}

/** The model list for new sessions of the kind: a blueprint pane reads the architect's. */
export function useChatModels(kind: ChatModelsKind = 'chat'): ChatModelsResult {
  const [loaded, setLoaded] = useState<{ kind: ChatModelsKind; result: ChatModelsResult } | null>(null);
  useEffect(() => {
    if (caches[kind].result) return;
    let cancelled = false;
    void loadChatModels(kind).then((result) => {
      if (cancelled) return;
      setLoaded({ kind, result });
    });
    return () => {
      cancelled = true;
    };
  }, [kind]);
  return caches[kind].result ?? (loaded?.kind === kind ? loaded.result : EMPTY);
}

export function modelMaxTokens(models: ChatModel[], modelId?: string): number {
  if (!modelId) return 0;
  return models.find((m) => m.id === modelId)?.max_tokens ?? 0;
}

// Threshold colors for context-window usage: neutral until 70%, amber 70-90%,
// red >= 90%. Used by PaneHeader.
export function usageColor(pct: number): string {
  if (pct >= 90) return 'var(--red)';
  if (pct >= 70) return 'var(--yellow)';
  return 'var(--grey1)';
}

export function contextPct(tokens: number, max: number): number {
  if (max <= 0) return 0;
  return Math.min(100, Math.round((tokens / max) * 100));
}

export function formatCostTooltip(tokens: {
  promptTokens?: number;
  completionTokens?: number;
  cacheReadTokens?: number;
  cacheCreationTokens?: number;
}): string {
  const segments: string[] = [];
  if (tokens.promptTokens !== undefined)
    segments.push(`Input: ${tokens.promptTokens.toLocaleString()}`);
  if (tokens.completionTokens !== undefined)
    segments.push(`Output: ${tokens.completionTokens.toLocaleString()}`);
  if (tokens.cacheReadTokens !== undefined)
    segments.push(`Cache read: ${tokens.cacheReadTokens.toLocaleString()}`);
  if (tokens.cacheCreationTokens !== undefined)
    segments.push(`Cache create: ${tokens.cacheCreationTokens.toLocaleString()}`);
  return segments.join(' · ');
}
