import { useCallback, useSyncExternalStore } from 'react';
import type { MockupSettings } from './frameProtocol';

// Module-level so flips survive the sheet closing and reopening, not a reload.
const flips = new Map<string, MockupSettings>();
const listeners = new Set<() => void>();

const keyOf = (chatId: string, revision: number) => `${chatId}/${revision}`;

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** The settings the person flipped to for a revision; null when nothing was flipped. */
export function flippedSettings(chatId: string, revision: number): MockupSettings | null {
  return flips.get(keyOf(chatId, revision)) ?? null;
}

/** Empties the store; tests start from it. */
export function resetMockupStore(): void {
  flips.clear();
  listeners.forEach((l) => l());
}

/** A revision's settings with the setter for one suggestion; `fixed` shows `opening` and ignores flips. */
export function useMockupSettings(
  chatId: string,
  revision: number,
  opening: MockupSettings,
  fixed: boolean,
): [MockupSettings, (id: string, value: string) => void] {
  const stored = useSyncExternalStore(subscribe, () => flips.get(keyOf(chatId, revision)));
  const current = fixed ? opening : (stored ?? opening);
  const set = useCallback(
    (id: string, value: string) => {
      const key = keyOf(chatId, revision);
      flips.set(key, { ...(flips.get(key) ?? opening), [id]: value });
      listeners.forEach((l) => l());
    },
    [chatId, revision, opening],
  );
  return [current, fixed ? noop : set];
}

function noop(): void {}
