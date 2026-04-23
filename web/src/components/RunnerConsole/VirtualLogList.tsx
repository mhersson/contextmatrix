import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import type { LogEntry } from '../../types';
import { LogLine } from './LogLine';

/**
 * Variable-height windowed list for log entries.
 *
 * Log row heights are NOT uniform (content can wrap via
 * `whitespace-pre-wrap break-words`), so we measure rows as they become visible
 * and cache their heights in an external store. Unmeasured rows use an
 * estimate; cumulative offsets are recomputed from the cache whenever it
 * changes. Binary search on the offsets array picks the visible window.
 *
 * Auto-scroll-to-bottom: when the user is within NEAR_BOTTOM_THRESHOLD of the
 * bottom, new items push the scroll to the new bottom.
 */

const ESTIMATE_ROW_HEIGHT = 24;
const BUFFER_ROWS = 10;
const NEAR_BOTTOM_THRESHOLD = 50;

interface HeightStore {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => number;
  setHeight: (idx: number, height: number) => void;
  getHeight: (idx: number) => number | undefined;
  clampTo: (length: number) => void;
}

function createHeightStore(): HeightStore {
  const cache = new Map<number, number>();
  const listeners = new Set<() => void>();
  let version = 0;

  function notify() {
    version++;
    for (const l of listeners) l();
  }

  return {
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    getSnapshot() {
      return version;
    },
    setHeight(idx, height) {
      if (height <= 0) return;
      if (cache.get(idx) === height) return;
      cache.set(idx, height);
      notify();
    },
    getHeight(idx) {
      return cache.get(idx);
    },
    clampTo(length) {
      let changed = false;
      for (const key of cache.keys()) {
        if (key >= length) {
          cache.delete(key);
          changed = true;
        }
      }
      if (changed) notify();
    },
  };
}

interface VirtualLogListProps {
  items: readonly LogEntry[];
  getKey: (item: LogEntry, index: number) => string;
  className?: string;
  emptyState?: React.ReactNode;
  // Accessibility props for the scrolling viewport — log surfaces use these
  // to announce updates to screen readers.
  role?: string;
  ariaLive?: 'off' | 'polite' | 'assertive';
  ariaAtomic?: boolean;
  ariaLabel?: string;
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
  role,
  ariaLive,
  ariaAtomic,
  ariaLabel,
}: VirtualLogListProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const heightStore = useMemo(() => createHeightStore(), []);
  const rowObserverRef = useRef<ResizeObserver | null>(null);
  // Maps the observed element back to its item index so the ResizeObserver
  // callback can update the height cache.
  const elementIndexRef = useRef<Map<Element, number>>(new Map());
  const userScrolledUpRef = useRef(false);

  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);
  // heightVersion increments whenever any row height is measured or updated.
  // Subscribing via useSyncExternalStore gives React a legal way to read
  // height-cache updates during render without violating the refs-during-render
  // rule. The returned version value is added to the offsets useMemo dep array
  // so cumulative offsets recompute whenever measured heights change.
  const heightVersion = useSyncExternalStore(heightStore.subscribe, heightStore.getSnapshot);

  // Set up the row ResizeObserver once. Row refs register/unregister into it.
  useEffect(() => {
    const elementIndex = elementIndexRef.current;
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const idx = elementIndex.get(entry.target);
        if (idx === undefined) continue;
        heightStore.setHeight(idx, entry.contentRect.height);
      }
    });
    rowObserverRef.current = ro;
    return () => {
      ro.disconnect();
      rowObserverRef.current = null;
      elementIndex.clear();
    };
  }, [heightStore]);

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
    heightStore.clampTo(items.length);
  }, [items.length, heightStore]);

  // Build cumulative offsets[] where offsets[i] is the top position of item i.
  // offsets has items.length + 1 entries; the last is the total height.
  // heightVersion is a versioned signal: the memo body reads heights via
  // heightStore.getHeight, but the ESLint exhaustive-deps rule flags heightVersion
  // as "unused inside the callback" because it is not referenced by name inside
  // the body. It IS necessary — without it the memo would not re-run after new
  // measurements land and offsets would remain stale at the 24px estimates.
  const offsets = useMemo(
    () => {
      const out = new Array<number>(items.length + 1);
      out[0] = 0;
      for (let i = 0; i < items.length; i++) {
        const h = heightStore.getHeight(i) ?? ESTIMATE_ROW_HEIGHT;
        out[i + 1] = out[i] + h;
      }
      return out;
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [items.length, heightStore, heightVersion],
  );

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
  const rowRef = useCallback(
    (idx: number) => (el: HTMLDivElement | null) => {
      const ro = rowObserverRef.current;
      if (!ro) return;
      const elementIndex = elementIndexRef.current;
      // Clean up any previous element that was mapped to this index.
      for (const [prevEl, prevIdx] of elementIndex) {
        if (prevIdx === idx && prevEl !== el) {
          ro.unobserve(prevEl);
          elementIndex.delete(prevEl);
        }
      }
      if (el) {
        elementIndex.set(el, idx);
        ro.observe(el);
        // Seed the cache immediately so first render has a measurement.
        const h = el.getBoundingClientRect().height;
        if (h > 0) heightStore.setHeight(idx, h);
      }
    },
    [heightStore],
  );

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
      data-testid="log-viewport"
      className={className}
      onScroll={handleScroll}
      style={{ overflow: 'auto', position: 'relative' }}
      role={role}
      aria-live={ariaLive}
      aria-atomic={ariaAtomic}
      aria-label={ariaLabel}
    >
      <div
        data-total-height={totalHeight}
        style={{ height: totalHeight, position: 'relative' }}
      >
        {visible}
      </div>
    </div>
  );
}
