import { useEffect, useSyncExternalStore } from 'react';
import { api } from '../../api/client';
import type { BlueprintArtifact } from '../../types';

export type ArtifactEntry =
  | { status: 'loading' }
  | { status: 'ready'; artifact: BlueprintArtifact }
  | { status: 'error' };

// Module-level like useChatLiveData: stored versions are immutable, and the
// pseudo-artifacts that change (sign-off, board) are refreshed on artifact_stored.
// A missed hint leaves the board stale, so every reader mount revalidates it too.
const entries = new Map<string, ArtifactEntry>();
const inflight = new Set<string>();
const stale = new Set<string>();
const listeners = new Set<() => void>();
const LOADING: ArtifactEntry = { status: 'loading' };

function keyOf(chatId: string, stage: string, version: number): string {
  return `${chatId}/${stage}/${version}`;
}

function notify(): void {
  listeners.forEach((l) => l());
}

/** Fetches an artifact into the cache; a ready entry stays shown until the new copy lands. */
export function loadArtifact(chatId: string, stage: string, version: number): void {
  const key = keyOf(chatId, stage, version);
  if (inflight.has(key)) return;
  inflight.add(key);
  if (entries.get(key)?.status !== 'ready') {
    entries.set(key, LOADING);
    notify();
  }
  api.getBlueprintArtifact(chatId, stage, version).then(
    (artifact) => {
      entries.set(key, { status: 'ready', artifact });
    },
    () => {
      if (entries.get(key)?.status !== 'ready') entries.set(key, { status: 'error' });
    },
  ).finally(() => {
    inflight.delete(key);
    notify();
    if (stale.delete(key)) loadArtifact(chatId, stage, version);
  });
}

/** Refetches an artifact someone has read; one stored again mid-fetch is fetched once more after. */
export function refreshArtifact(chatId: string, stage: string, version: number): void {
  const key = keyOf(chatId, stage, version);
  if (!entries.has(key)) return;
  if (inflight.has(key)) {
    stale.add(key);
    return;
  }
  loadArtifact(chatId, stage, version);
}

/** Empties the cache; tests start from it. */
export function resetArtifactCache(): void {
  entries.clear();
  inflight.clear();
  stale.clear();
  notify();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useBlueprintArtifact(chatId: string, stage: string, version: number | null): ArtifactEntry | null {
  const key = version === null ? null : keyOf(chatId, stage, version);
  const entry = useSyncExternalStore(subscribe, () => (key ? entries.get(key) : undefined));
  useEffect(() => {
    if (key !== null && version !== null && (!entries.has(key) || stage === 'emission')) {
      loadArtifact(chatId, stage, version);
    }
  }, [key, chatId, stage, version]);
  if (key === null) return null;
  return entry ?? LOADING;
}
