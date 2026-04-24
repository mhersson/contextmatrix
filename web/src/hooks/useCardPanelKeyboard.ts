import { useEffect, useRef } from 'react';

/**
 * Registers a global `keydown` listener for the CardPanel that dispatches to
 * the caller's close and (optional) save handlers.
 *
 * - **Escape** → calls `onClose()`. The caller decides whether to prompt for
 *   discard (typically via `ConfirmModal`); the hook itself doesn't confirm.
 * - **⌘S / Ctrl+S** → calls `onSave()` and prevents the browser's default
 *   save dialog. No-op when `onSave` is omitted.
 *
 * The listener is registered **exactly once per mount** in a mount-only
 * `useEffect`. Both handlers are captured through refs so the listener can
 * read the latest values without re-registering on every render. This avoids
 * the bug where a `deps: [onClose, onSave]` array would re-attach the listener
 * on every keystroke in the editor because the callbacks are rebuilt upstream.
 */
export function useCardPanelKeyboard(
  onClose: () => void,
  onSave?: () => void | Promise<void>,
): void {
  const onCloseRef = useRef(onClose);
  const onSaveRef = useRef(onSave);

  useEffect(() => {
    onCloseRef.current = onClose;
    onSaveRef.current = onSave;
  });

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onCloseRef.current();
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
        if (!onSaveRef.current) return;
        e.preventDefault();
        void onSaveRef.current();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, []);
}
