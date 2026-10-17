import { useSyncExternalStore } from 'react';

export interface SheetButtonState {
  open: boolean;
  hasNew: boolean;
}

// Module-level like useChatLiveData: the pane header is a sibling above the
// pane body, so the phone Sheet button and the sheet meet here.
const states = new Map<string, SheetButtonState>();
const toggles = new Map<string, () => void>();
const listeners = new Set<() => void>();

function notify(): void {
  listeners.forEach((l) => l());
}

/** A phone blueprint pane publishes its sheet state for the header button; null withdraws it. */
export function publishSheetButton(chatId: string, state: SheetButtonState | null): void {
  const prev = states.get(chatId);
  if (state === null) {
    if (prev) {
      states.delete(chatId);
      notify();
    }
    return;
  }
  if (prev && prev.open === state.open && prev.hasNew === state.hasNew) return;
  states.set(chatId, state);
  notify();
}

export function registerSheetToggle(chatId: string, toggle: () => void): () => void {
  toggles.set(chatId, toggle);
  return () => {
    if (toggles.get(chatId) === toggle) toggles.delete(chatId);
  };
}

export function toggleSheet(chatId: string): void {
  toggles.get(chatId)?.();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useSheetButton(chatId: string | null): SheetButtonState | undefined {
  return useSyncExternalStore(
    subscribe,
    () => (chatId ? states.get(chatId) : undefined),
    () => undefined,
  );
}
