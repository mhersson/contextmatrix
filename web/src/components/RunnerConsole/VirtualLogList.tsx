import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { LogEntry } from '../../types';
import { LogLine } from './LogLine';

/**
 * Variable-height windowed list for log entries.
 *
 * Log row heights are NOT uniform (content can wrap via
 * `whitespace-pre-wrap break-words`), so we measure rows as they become visible
 * and cache their heights. Unmeasured rows use an estimate; cumulative offsets
 * are recomputed from the cache whenever it changes. Binary search on the
 * offsets array picks the visible window.
 *
 * Auto-scroll-to-bottom: when the user is within NEAR_BOTTOM_THRESHOLD of the
 * bottom, new items push the scroll to the new bottom.
 */

const ESTIMATE_ROW_HEIGHT = 24;
const BUFFER_ROWS = 10;
const NEAR_BOTTOM_THRESHOLD = 50;

interface VirtualLogListProps {
  items: readonly LogEntry[];
  getKey: (item: LogEntry, index: number) => string;
  className?: string;
  emptyState?: React.ReactNode;
}

/**
 * Binary search for the first index whose offset is >= target.
 * offsets is monotonically increasing. Returns offsets.length if none.
 */
function firstOffsetAtLeast(offsets: number[], target: number): number {
  let lo = 0;
  let hi = offsets.length;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1;
    if (offsets[mid] < target) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

export function VirtualLogList({
  items,
  getKey,
  className,
  emptyState,
}: VirtualLogListProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const heightCacheRef = useRef<Map<number, number>>(new Map());
  const rowObserverRef = useRef<ResizeObserver | null>(null);
  // Maps the observed element back to its item index so the ResizeObserver
  // callback can update the height cache.
  const elementIndexRef = useRef<Map<Element, number>>(new Map());
  const userScrolledUpRef = useRef(false);

  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);
  // Version counter used to trigger re-layout when the height cache changes.
  const [cacheVersion, setCacheVersion] = useState(0);

  // Set up the row ResizeObserver once. Row refs register/unregister into it.
  useEffect(() => {
    const ro = new ResizeObserver((entries) => {
      let changed = false;
      for (const entry of entries) {
        const idx = elementIndexRef.current.get(entry.target);
        if (idx === undefined) continue;
        const height = entry.contentRect.height;
        if (height <= 0) continue;
        const prev = heightCacheRef.current.get(idx);
        if (prev !== height) {
          heightCacheRef.current.set(idx, height);
          changed = true;
        }
      }
      if (changed) setCacheVersion((v) => v + 1);
    });
    rowObserverRef.current = ro;
    return () => {
      ro.disconnect();
      rowObserverRef.current = null;
      elementIndexRef.current.clear();
    };
  }, []);

  // Track viewport height via ResizeObserver.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      if (viewportRef.current) {
        setViewportHeight(viewportRef.current.clientHeight);
      }
    });
    ro.observe(el);
    setViewportHeight(el.clientHeight);
    return () => ro.disconnect();
  }, []);

  // Drop height-cache entries for indices beyond the current items length so
  // the ring buffer's drop-oldest behaviour doesn't leak stale measurements.
  useEffect(() => {
    const cache = heightCacheRef.current;
    let changed = false;
    for (const key of cache.keys()) {
      if (key >= items.length) {
        cache.delete(key);
        changed = true;
      }
    }
    if (changed) setCacheVersion((v) => v + 1);
  }, [items.length]);

  // Build cumulative offsets[] where offsets[i] is the top position of item i.
  // offsets has items.length + 1 entries; the last is the total height.
  const offsets = useMemo(() => {
    const out = new Array<number>(items.length + 1);
    out[0] = 0;
    const cache = heightCacheRef.current;
    for (let i = 0; i < items.length; i++) {
      const h = cache.get(i) ?? ESTIMATE_ROW_HEIGHT;
      out[i + 1] = out[i] + h;
    }
    return out;
    // cacheVersion is a proxy for cache mutations since the Map identity is stable.
  }, [items.length, cacheVersion]);

  const totalHeight = offsets[items.length] ?? 0;

  // Compute the visible slice using binary search. Add BUFFER_ROWS above/below.
  const { startIndex, endIndex } = useMemo(() => {
    if (items.length === 0) return { startIndex: 0, endIndex: 0 };
    const viewTop = scrollTop;
    const viewBottom = scrollTop + viewportHeight;
    let start = firstOffsetAtLeast(offsets, viewTop) - 1;
    if (start < 0) start = 0;
    let end = firstOffsetAtLeast(offsets, viewBottom);
    if (end > items.length) end = items.length;
    start = Math.max(0, start - BUFFER_ROWS);
    end = Math.min(items.length, end + BUFFER_ROWS);
    return { startIndex: start, endIndex: end };
  }, [items.length, offsets, scrollTop, viewportHeight]);

  const handleScroll = useCallback(() => {
    const el = viewportRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
    setScrollTop(el.scrollTop);
  }, []);

  // Auto-scroll to bottom when items change and user hasn't scrolled up.
  useLayoutEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    if (userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [items, totalHeight]);

  // Register/unregister a row element with the ResizeObserver.
  const rowRef = useCallback((idx: number) => (el: HTMLDivElement | null) => {
    const ro = rowObserverRef.current;
    if (!ro) return;
    // Clean up any previous element that was mapped to this index.
    for (const [prevEl, prevIdx] of elementIndexRef.current) {
      if (prevIdx === idx && prevEl !== el) {
        ro.unobserve(prevEl);
        elementIndexRef.current.delete(prevEl);
      }
    }
    if (el) {
      elementIndexRef.current.set(el, idx);
      ro.observe(el);
      // Seed the cache immediately so first render has a measurement.
      const h = el.getBoundingClientRect().height;
      if (h > 0 && heightCacheRef.current.get(idx) !== h) {
        heightCacheRef.current.set(idx, h);
        setCacheVersion((v) => v + 1);
      }
    }
  }, []);

  if (items.length === 0 && emptyState) {
    return <>{emptyState}</>;
  }

  const visible: React.ReactElement[] = [];
  for (let i = startIndex; i < endIndex; i++) {
    const item = items[i];
    visible.push(
      <div
        key={getKey(item, i)}
        ref={rowRef(i)}
        data-testid="log-line"
        style={{ position: 'absolute', top: offsets[i], left: 0, right: 0 }}
      >
        <LogLine entry={item} />
      </div>,
    );
  }

  return (
    <div
      ref={viewportRef}
      className={className}
      onScroll={handleScroll}
      style={{ overflow: 'auto', position: 'relative' }}
    >
      <div style={{ height: totalHeight, position: 'relative' }}>{visible}</div>
    </div>
  );
}
