import { useCallback, useEffect, useId, useRef, useState, type FocusEvent } from 'react';

/**
 * Open state and dismissal for a strip popover: Escape, a mousedown outside
 * the container or focus leaving it closes it. `onClose` runs on every close.
 */
export function usePopover(onClose?: () => void) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();

  const close = useCallback(() => {
    setOpen(false);
    onClose?.();
  }, [onClose]);

  useEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      if (containerRef.current?.contains(e.target as Node)) return;
      // Focus would otherwise drop to <body> with the popover it sat in.
      if (containerRef.current?.contains(document.activeElement)) triggerRef.current?.focus();
      close();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      // One Escape closes only the popover, not the mobile drawer's window listener too.
      e.stopPropagation();
      triggerRef.current?.focus();
      close();
    };
    document.addEventListener('mousedown', onMouseDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onMouseDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open, close]);

  const toggle = useCallback(() => (open ? close() : setOpen(true)), [open, close]);

  // A null relatedTarget (window blur, a click on plain text) is not leaving.
  const onBlur = (e: FocusEvent<HTMLDivElement>) => {
    if (open && e.relatedTarget && !e.currentTarget.contains(e.relatedTarget)) close();
  };

  return { open, toggle, containerRef, triggerRef, popoverId, onBlur };
}
