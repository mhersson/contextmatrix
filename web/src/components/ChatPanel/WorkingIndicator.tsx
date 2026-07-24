import { useEffect, useState } from 'react';

// eslint-disable-next-line react-refresh/only-export-components
export function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}m ${String(s).padStart(2, '0')}s`;
}

interface WorkingIndicatorProps {
  verb: string;
  /** Epoch millis the in-flight turn started. */
  since: number;
}

/**
 * Typing-indicator row rendered as the last entry of the chat thread while a
 * turn is in flight. The ticking text is aria-hidden so the polite live region
 * wrapping the log does not announce every second; a visually-hidden static
 * status line announces once instead.
 */
export function WorkingIndicator({ verb, since }: WorkingIndicatorProps) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const elapsed = Math.max(0, Math.floor((now - since) / 1000));

  return (
    <div
      className="flex items-center gap-2 text-xs font-mono"
      data-testid="working-indicator"
    >
      <span aria-hidden="true" className="flex items-center gap-2" style={{ color: 'var(--grey1)' }}>
        <span className="animate-pulse" style={{ color: 'var(--aqua)' }}>✳</span>
        <span>
          {verb}… ({formatElapsed(elapsed)})
        </span>
      </span>
      <span className="sr-only" role="status">
        Assistant is working
      </span>
    </div>
  );
}
