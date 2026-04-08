import { useState, useRef, useCallback, useEffect } from 'react';

interface UseResizeDividerOptions {
  containerRef: React.RefObject<HTMLElement | null>;
  enabled: boolean;
  minBoard?: number;
  minConsole?: number;
}

interface UseResizeDividerResult {
  boardPercent: number;
  isDragging: boolean;
  handleProps: {
    onPointerDown: (e: React.PointerEvent) => void;
    onPointerMove: (e: React.PointerEvent) => void;
    onPointerUp: (e: React.PointerEvent) => void;
    style: React.CSSProperties;
  };
}

const DEFAULT_BOARD = 60;

export function useResizeDivider({
  containerRef,
  enabled,
  minBoard = 20,
  minConsole = 15,
}: UseResizeDividerOptions): UseResizeDividerResult {
  const [boardPercent, setBoardPercent] = useState(DEFAULT_BOARD);
  const [isDragging, setIsDragging] = useState(false);
  const draggingRef = useRef(false);
  const startYRef = useRef(0);
  const startPercentRef = useRef(DEFAULT_BOARD);

  const restoreBodyStyles = useCallback(() => {
    document.body.style.userSelect = '';
    document.body.style.cursor = '';
  }, []);

  useEffect(() => {
    return restoreBodyStyles;
  }, [restoreBodyStyles]);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (!enabled || !containerRef.current) return;
      (e.target as HTMLElement).setPointerCapture(e.pointerId);
      draggingRef.current = true;
      setIsDragging(true);
      startYRef.current = e.clientY;
      startPercentRef.current = boardPercent;
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'row-resize';
    },
    [enabled, containerRef, boardPercent]
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent) => {
      if (!draggingRef.current || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const deltaY = e.clientY - startYRef.current;
      const deltaPct = (deltaY / rect.height) * 100;
      const clamped = Math.max(minBoard, Math.min(100 - minConsole, startPercentRef.current + deltaPct));
      setBoardPercent(Math.round(clamped));
    },
    [containerRef, minBoard, minConsole]
  );

  const onPointerUp = useCallback(
    (e: React.PointerEvent) => {
      if (!draggingRef.current) return;
      (e.target as HTMLElement).releasePointerCapture(e.pointerId);
      draggingRef.current = false;
      setIsDragging(false);
      restoreBodyStyles();
    },
    [restoreBodyStyles]
  );

  return {
    boardPercent: enabled ? boardPercent : DEFAULT_BOARD,
    isDragging,
    handleProps: {
      onPointerDown,
      onPointerMove,
      onPointerUp,
      style: { touchAction: 'none' },
    },
  };
}
