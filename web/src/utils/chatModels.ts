import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { ChatModel } from '../types';

// Module-scope cache so a single GET /api/chats/models response is reused by
// every component that needs the model list (PaneHeader, etc).
const cache: { promise?: Promise<ChatModel[]>; models?: ChatModel[] } = {};

export function loadChatModels(): Promise<ChatModel[]> {
  if (cache.models) return Promise.resolve(cache.models);
  if (cache.promise) return cache.promise;
  cache.promise = api
    .listChatModels()
    .then((resp) => {
      cache.models = resp.models;
      return resp.models;
    })
    .catch(() => []);
  return cache.promise;
}

export function useChatModels(): ChatModel[] {
  const [models, setModels] = useState<ChatModel[]>(cache.models ?? []);
  useEffect(() => {
    if (cache.models) return;
    let cancelled = false;
    void loadChatModels().then((list) => {
      if (cancelled) return;
      setModels(list);
    });
    return () => {
      cancelled = true;
    };
  }, []);
  return models;
}

export function modelMaxTokens(models: ChatModel[], modelId?: string): number {
  if (!modelId) return 0;
  return models.find((m) => m.id === modelId)?.max_tokens ?? 0;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 10_000) return `${Math.round(n / 1_000)}k`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
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
