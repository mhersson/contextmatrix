import { useEffect, useRef } from 'react';
import type { LogEntry } from '../../types';
import { LogLine } from './LogLine';

interface RunnerConsoleLogProps {
  logs: readonly LogEntry[];
  error: string | null;
}

const NEAR_BOTTOM_THRESHOLD = 50;

export function RunnerConsoleLog({ logs, error }: RunnerConsoleLogProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const userScrolledUpRef = useRef(false);
  // Throttle scroll measurements to once per animation frame so fast native
  // scroll events do not queue up O(n) work in the React render loop.
  const rafIdRef = useRef<number | null>(null);

  const handleScroll = () => {
    if (rafIdRef.current !== null) return;
    rafIdRef.current = requestAnimationFrame(() => {
      rafIdRef.current = null;
      const el = containerRef.current;
      if (!el) return;
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
    });
  };

  useEffect(() => {
    return () => {
      if (rafIdRef.current !== null) cancelAnimationFrame(rafIdRef.current);
    };
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el || userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [logs]);

  if (logs.length === 0) {
    return (
      <div
        className="flex-1 flex items-center justify-center text-xs"
        style={{ color: error ? 'var(--red)' : 'var(--grey1)' }}
      >
        {error ?? 'No log entries'}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="flex-1 overflow-y-auto min-h-0"
      onScroll={handleScroll}
    >
      {logs.map((entry, idx) => (
        <LogLine key={`${entry.ts}-${entry.card_id}-${idx}`} entry={entry} />
      ))}
    </div>
  );
}
