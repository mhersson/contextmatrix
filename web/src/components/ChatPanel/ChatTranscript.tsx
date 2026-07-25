import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { LogEntry } from '../../types';
import type { WorkingState } from '../../hooks/useWorkingState';
import { useRevealWindow } from '../../hooks/useRevealWindow';
import { logRowKey } from '../../utils/logRowKey';
import { ChatEntry } from './ChatEntry';
import { WorkingIndicator } from './WorkingIndicator';
import { decorateLogs } from './decorateLogs';
import { INITIAL_TAIL, REVEAL_CHUNK } from './chatEntryUtils';

const NEAR_BOTTOM_THRESHOLD = 50;
/** Scrolling within this many px of the top auto-reveals the next chunk. */
const REVEAL_SCROLL_THRESHOLD = 200;

interface ChatTranscriptProps {
  filteredLogs: readonly LogEntry[];
  working?: WorkingState | null;
}

interface PendingAnchor {
  key: string;
  prevTop: number;
}

function findRowByKey(container: HTMLElement, key: string): Element | null {
  for (const row of container.querySelectorAll('[data-logkey]')) {
    if (row.getAttribute('data-logkey') === key) return row;
  }
  return null;
}

/**
 * The scrolling transcript column: renders a tail window over the filtered
 * logs (older messages revealed on demand), pins to the bottom while the user
 * is there, and keeps the viewport position stable when older content is
 * prepended above it. While the user reads history the reveal window holds
 * its top so live appends cannot slide rows out from under them.
 */
export function ChatTranscript({ filteredLogs, working }: ChatTranscriptProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const userScrolledUpRef = useRef(false);
  // State mirror of userScrolledUpRef: drives the reveal window's holdTop
  // policy, which must participate in render. The ref stays the synchronous
  // source for scroll-effect decisions.
  const [scrolledUp, setScrolledUp] = useState(false);
  /** Set while a reveal is in flight: remembers which row to re-anchor to
   *  after commit, and doubles as the reentry guard for auto-reveal. */
  const pendingAnchorRef = useRef<PendingAnchor | null>(null);

  const { visible, hiddenCount, revealMore } = useRevealWindow(
    filteredLogs,
    INITIAL_TAIL,
    REVEAL_CHUNK,
    scrolledUp,
  );
  const decoratedLogs = useMemo(() => decorateLogs(visible), [visible]);

  // Warm the markdown chunk as soon as the transcript mounts so the
  // per-message Suspense fallback window is as short as possible.
  useEffect(() => {
    void import('@uiw/react-markdown-preview');
  }, []);

  const triggerReveal = useCallback(() => {
    const el = containerRef.current;
    if (!el || pendingAnchorRef.current) return;
    const firstRow = el.querySelector('[data-logkey]');
    if (firstRow) {
      pendingAnchorRef.current = {
        key: firstRow.getAttribute('data-logkey') ?? '',
        prevTop: firstRow.getBoundingClientRect().top,
      };
    }
    revealMore();
  }, [revealMore]);

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const isScrolledUp = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
    userScrolledUpRef.current = isScrolledUp;
    setScrolledUp(isScrolledUp);
    // Auto-reveal only on a genuine user scroll into history (isScrolledUp
    // filters out the scroll events fired by the programmatic bottom-pin
    // when the tail barely overflows the viewport).
    if (
      isScrolledUp &&
      el.scrollTop < REVEAL_SCROLL_THRESHOLD &&
      hiddenCount > 0 &&
      !pendingAnchorRef.current
    ) {
      triggerReveal();
    }
  }, [hiddenCount, triggerReveal]);

  // Runs pre-paint after every visible change: either restore the anchor row
  // to its previous viewport offset (reveal just prepended content above it),
  // or pin to the bottom when the user hasn't scrolled away.
  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const pending = pendingAnchorRef.current;
    if (pending) {
      pendingAnchorRef.current = null;
      const row = findRowByKey(el, pending.key);
      if (row) {
        el.scrollTop += row.getBoundingClientRect().top - pending.prevTop;
      }
      // Revealing means the user is reading history - never yank to bottom
      // here, even when the anchor row got evicted between trigger and
      // commit (a rare ring-drop race; position is then left to native
      // scroll anchoring rather than teleporting to the tail).
      userScrolledUpRef.current = true;
      setScrolledUp(true);
      return;
    }
    if (userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [visible, working]);

  // Heights change without a `visible` identity change when the lazy markdown
  // chunk resolves, images load, or a payload is expanded - re-pin to the
  // bottom in those cases so the tail stays in view. The observed wrapper is
  // always mounted (it also hosts the empty state), so the [] deps are safe.
  useEffect(() => {
    const el = containerRef.current;
    const content = contentRef.current;
    if (!el || !content || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => {
      if (userScrolledUpRef.current || pendingAnchorRef.current) return;
      el.scrollTop = el.scrollHeight;
    });
    ro.observe(content);
    return () => ro.disconnect();
  }, []);

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      data-testid="chat-scroller"
      className="flex-1 min-h-[60px] overflow-y-auto overflow-x-hidden px-4 py-4 bg-[var(--bg-dim)]"
    >
      {/* The show-older affordance lives OUTSIDE the live region so its
          hidden-count label churn is never announced by screen readers. */}
      {hiddenCount > 0 && (
        <button
          type="button"
          data-testid="chat-show-older"
          onClick={triggerReveal}
          className="block w-full text-center text-xs font-mono py-1.5 mb-3 rounded border cursor-pointer"
          style={{
            color: 'var(--grey1)',
            borderColor: 'var(--bg3)',
            backgroundColor: 'var(--bg1)',
          }}
        >
          Show older messages ({hiddenCount} hidden)
        </button>
      )}
      <div
        ref={contentRef}
        className="space-y-3"
        role="log"
        aria-live="polite"
        aria-label="Chat log"
      >
        {decoratedLogs.length === 0 && !working ? (
          <div className="text-xs text-[var(--grey1)] italic font-mono">No messages yet.</div>
        ) : (
          <>
            {decoratedLogs.map((d) => (
              <div key={logRowKey(d.entry)} data-logkey={logRowKey(d.entry)}>
                <ChatEntry
                  entry={d.entry}
                  stampHHMM={d.showStamp ? d.hhmm : undefined}
                  stampTitle={d.showStamp ? d.title : undefined}
                />
              </div>
            ))}
            {working && <WorkingIndicator verb={working.verb} since={working.since} />}
          </>
        )}
      </div>
    </div>
  );
}
