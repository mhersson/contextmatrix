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
 * older ones are revealed in `chunk`-sized steps on demand.
 *
 * Append policy depends on `holdTop`:
 * - `holdTop === false` (reader at the live tail): the window is a capped
 *   slice that slides with appends - old rows fall back behind the fold,
 *   which is invisible from the bottom and keeps the DOM bounded.
 * - `holdTop === true` (reader scrolled up into history): the window start is
 *   pinned, appends grow the visible slice at the bottom, and revealed rows
 *   never slide out from under the reader.
 *
 * When the list shrinks (clear on reconnect, filter toggled off) the revealed
 * extent resets so a fresh stream starts from a tail again.
 */
export function useRevealWindow<T>(
  items: readonly T[],
  initialTail: number,
  chunk: number,
  holdTop = false,
): RevealWindow<T> {
  const [extraRevealed, setExtraRevealed] = useState(0);

  // Adjust synchronously in render when the list length changes - same
  // pattern as the sessionID reset in ChatThread (see web/CLAUDE.md on
  // render-time resets).
  const [prevLen, setPrevLen] = useState(items.length);
  const [prevFirst, setPrevFirst] = useState<T | undefined>(items[0]);
  if (items.length !== prevLen) {
    setPrevLen(items.length);
    setPrevFirst(items[0]);
    if (items.length < prevLen) {
      if (extraRevealed !== 0) {
        setExtraRevealed(0);
      }
    } else {
      // Growth at the TOP (first item identity changed) is a history-page
      // prepend: widen the extent so the fetched rows are visible instead
      // of landing behind the fold. Growth at the bottom widens only while
      // reading history (holdTop), so the window start stays put; at the
      // live tail it slides, keeping the DOM bounded.
      const grewOnTop = prevLen > 0 && items[0] !== prevFirst;
      if (holdTop || grewOnTop) {
        const grown = extraRevealed + (items.length - prevLen);
        setExtraRevealed(Math.min(grown, Math.max(0, items.length - initialTail)));
      }
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
