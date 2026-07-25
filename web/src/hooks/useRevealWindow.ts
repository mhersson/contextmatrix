import { useCallback, useMemo, useState } from 'react';

export interface RevealWindow<T> {
  /** The tail slice currently eligible for rendering. */
  visible: readonly T[];
  /** How many older items are hidden above the window. */
  hiddenCount: number;
  /** Widen the window upward by one chunk. */
  revealMore: () => void;
}

/**
 * Tail window over a growing list: the newest `initialTail` items are visible,
 * older ones are revealed in `chunk`-sized steps on demand. Live appends grow
 * the visible slice at the bottom without sliding the top past revealed
 * content. When the list shrinks (clear on reconnect, filter toggled off) the
 * revealed extent resets so a fresh stream starts from a tail again.
 */
export function useRevealWindow<T>(
  items: readonly T[],
  initialTail: number,
  chunk: number,
): RevealWindow<T> {
  const [extraRevealed, setExtraRevealed] = useState(0);

  // Reset synchronously in render when the list shrinks - same pattern as the
  // sessionID reset in ChatThread (see web/CLAUDE.md on render-time resets).
  const [prevLen, setPrevLen] = useState(items.length);
  if (items.length !== prevLen) {
    setPrevLen(items.length);
    if (items.length < prevLen && extraRevealed !== 0) {
      setExtraRevealed(0);
    }
  }

  const startIndex = Math.max(0, items.length - initialTail - extraRevealed);

  const visible = useMemo(
    () => (startIndex === 0 ? items : items.slice(startIndex)),
    [items, startIndex],
  );

  const maxExtra = Math.max(0, items.length - initialTail);
  const revealMore = useCallback(() => {
    setExtraRevealed((prev) => Math.min(prev + chunk, maxExtra));
  }, [chunk, maxExtra]);

  return { visible, hiddenCount: startIndex, revealMore };
}
