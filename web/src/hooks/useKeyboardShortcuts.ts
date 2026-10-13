import { useEffect } from 'react';

export interface Shortcut {
  key: string;
  /** Return false to decline: the event is then left unconsumed for other listeners. */
  handler: () => boolean | void;
}

function isInputElement(el: EventTarget | null): boolean {
  if (!el || !(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  return (
    tag === 'INPUT' ||
    tag === 'TEXTAREA' ||
    tag === 'SELECT' ||
    el.isContentEditable
  );
}

export function useKeyboardShortcuts(shortcuts: Shortcut[]) {
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isInputElement(e.target)) return;

      for (const shortcut of shortcuts) {
        if (e.key === shortcut.key) {
          if (shortcut.handler() !== false) e.preventDefault();
          return;
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [shortcuts]);
}
