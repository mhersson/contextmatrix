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
 * Growth policy:
 * - `holdTop === true` (reader scrolled up into history): the window start is
 *   pinned, appends grow the visible slice at the bottom, and revealed rows
 *   never slide out from under the reader.
 * - `expectTopGrowth === true` AND the first item's identity changed: the
 *   growth is a history-page prepend - widen so the fetched rows are visible
 *   instead of landing behind the fold. The flag must come from the caller
 *   (a fetch is/was in flight): first-item identity alone cannot distinguish
 *   a prepend from a filter toggle re-including old rows, and widening on
 *   toggles would mount the whole backlog and unbound the DOM.
 * - otherwise (reader at the live tail): the window is a capped slice that
 *   slides with appends - old rows fall back behind the fold, which is
 *   invisible from the bottom and keeps the DOM bounded.
 *
 * When the list shrinks (clear on reconnect, filter toggled off) the revealed
 * extent resets so a fresh stream starts from a tail again.
 */
export function useRevealWindow<T>(
  items: readonly T[],
  initialTail: number,
  chunk: number,
  holdTop = false,
  expectTopGrowth = false,
): RevealWindow<T> {
  const [extraRevealed, setExtraRevealed] = useState(0);

  // Adjust synchronously in render when the list identity changes - same
  // pattern as the sessionID reset in ChatThread (see web/AGENTS.md on
  // render-time resets). The whole previous array is the baseline (not just
  // its length): at ring capacity an append changes items[0] with the length
  // unchanged, and a length-gated baseline would go stale and misclassify
  // later growth.
  const [prevItems, setPrevItems] = useState(items);
  if (items !== prevItems) {
    setPrevItems(items);
    if (items.length < prevItems.length) {
      if (extraRevealed !== 0) {
        setExtraRevealed(0);
      }
    } else if (items.length > prevItems.length) {
      const grewOnTop =
        expectTopGrowth && prevItems.length > 0 && items[0] !== prevItems[0];
      if (holdTop || grewOnTop) {
        const grown = extraRevealed + (items.length - prevItems.length);
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
