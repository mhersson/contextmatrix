import { useEffect, type RefObject } from 'react';

/**
 * Closes a popover menu on Escape or on a mousedown outside `containerRef`.
 * Pass `returnFocusTo` (the trigger) when the menu unmounts on close: if focus
 * was inside the menu at that moment it would otherwise drop to <body>, so the
 * trigger is refocused first - keyboard users continue from where they opened.
 */
export function useMenuDismiss(
  containerRef: RefObject<HTMLElement | null>,
  open: boolean,
  onClose: () => void,
  returnFocusTo?: RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!open) return;

    const close = () => {
      if (returnFocusTo && containerRef.current?.contains(document.activeElement)) {
        returnFocusTo.current?.focus();
      }
      onClose();
    };
    const handleMouseDown = (e: MouseEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) close();
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close();
    };

    document.addEventListener('mousedown', handleMouseDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleMouseDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open, containerRef, onClose, returnFocusTo]);
}
