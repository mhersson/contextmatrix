import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type RefObject } from 'react';

const START_PERCENT = 46;
const MIN_SIDE_PX = 320;
const KEY_STEP = 2;

/** The sheet's share of the pane body, set by dragging the divider; never stored. */
export function useSheetDivider(containerRef: RefObject<HTMLElement | null>) {
  const [percent, setPercent] = useState(START_PERCENT);
  const draggingRef = useRef(false);

  const clamp = useCallback(
    (p: number) => {
      const width = containerRef.current?.getBoundingClientRect().width ?? 0;
      if (width <= 0) return p;
      const min = (MIN_SIDE_PX / width) * 100;
      return Math.min(100 - min, Math.max(min, p));
    },
    [containerRef],
  );

  const restoreBody = useCallback(() => {
    document.body.style.userSelect = '';
    document.body.style.cursor = '';
  }, []);
  useEffect(() => restoreBody, [restoreBody]);

  const onPointerDown = (e: PointerEvent<HTMLElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    draggingRef.current = true;
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
  };
  const onPointerMove = (e: PointerEvent<HTMLElement>) => {
    const rect = containerRef.current?.getBoundingClientRect();
    if (!draggingRef.current || !rect || rect.width <= 0) return;
    setPercent(clamp(((rect.right - e.clientX) / rect.width) * 100));
  };
  const onPointerEnd = (e: PointerEvent<HTMLElement>) => {
    if (!draggingRef.current) return;
    draggingRef.current = false;
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
    restoreBody();
  };
  // The sheet is on the right: left widens it, right narrows it.
  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      setPercent((p) => clamp(p + KEY_STEP));
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      setPercent((p) => clamp(p - KEY_STEP));
    }
  };

  return {
    percent,
    separatorProps: {
      role: 'separator' as const,
      'aria-orientation': 'vertical' as const,
      'aria-label': 'Resize the sheet',
      'aria-valuenow': Math.round(percent),
      'aria-valuemin': 0,
      'aria-valuemax': 100,
      tabIndex: 0,
      onPointerDown,
      onPointerMove,
      onPointerUp: onPointerEnd,
      onPointerCancel: onPointerEnd,
      onKeyDown,
    },
  };
}
