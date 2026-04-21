import { useEffect, useRef } from 'react';

/**
 * Registers a global `keydown` listener for the CardPanel that handles Escape
 * (with a discard-prompt when the card is dirty).
 *
 * The listener is registered **exactly once per mount** in a mount-only
 * `useEffect`. `isDirty` and `onClose` are captured through refs so the
 * listener can read their latest values without re-registering on every
 * keystroke. This avoids the previous bug where including `isDirty` in the
 * effect's dep array re-attached the listener on every character typed into
 * the editor.
 */
export function useCardPanelKeyboard(isDirty: boolean, onClose: () => void): void {
  const isDirtyRef = useRef(isDirty);
  const onCloseRef = useRef(onClose);

  // Keep refs in sync with the latest props. Writing refs from an effect
  // satisfies the `react-hooks/refs` rule (no ref writes during render) while
  // still letting the mount-only keydown listener read the latest values.
  useEffect(() => {
    isDirtyRef.current = isDirty;
    onCloseRef.current = onClose;
  });

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Escape') return;
      if (isDirtyRef.current) {
        if (window.confirm('Discard unsaved changes?')) onCloseRef.current();
      } else {
        onCloseRef.current();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, []);
}
