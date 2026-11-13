import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useSentinel } from '../../hooks/useSentinel';
import { stripText, stripTone } from '../../lib/sentinel';
import { ShieldCheckIcon } from '../ShieldCheckIcon';
import { SentinelDetails } from './SentinelDetails';

function Chevron() {
  return (
    <svg aria-hidden="true" width="9" height="9" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 6l5 5 5-5" />
    </svg>
  );
}

/** Admin in multi mode, anyone in none mode. UX only: the settings page is admin-gated server-side. */
function useIsAdmin(): boolean {
  const auth = useOptionalAuth();
  return auth?.mode !== 'multi' || Boolean(auth?.user?.is_admin);
}

/**
 * Brand-header strip for Sentinel with a popover of details, shown only while
 * it is enabled. Sits under the LLM channel strip, so the brand header must
 * stay `relative` for the popover's positioning.
 */
export function SentinelStrip() {
  const { info } = useSentinel();
  const isAdmin = useIsAdmin();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();

  const close = useCallback(() => setOpen(false), []);

  useEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      if (containerRef.current?.contains(e.target as Node)) return;
      if (containerRef.current?.contains(document.activeElement)) stripRef.current?.focus();
      close();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      // One Escape closes only the popover, not the mobile drawer's window listener too.
      e.stopPropagation();
      stripRef.current?.focus();
      close();
    };
    document.addEventListener('mousedown', onMouseDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onMouseDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open, close]);

  const status = info?.status;
  if (!status?.enabled) return null;

  const text = stripText(status);
  return (
    <div
      ref={containerRef}
      onBlur={(e) => {
        if (open && e.relatedTarget && !e.currentTarget.contains(e.relatedTarget)) close();
      }}
    >
      <button
        ref={stripRef}
        type="button"
        className="llm-strip"
        aria-label={`Sentinel: ${text}`}
        aria-expanded={open}
        aria-controls={popoverId}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <span className={`sentinel-tone--${stripTone(status)} inline-flex`} aria-hidden="true">
          <ShieldCheckIcon />
        </span>
        <span className="llm-strip__label">Sentinel</span>
        <span className={`llm-strip__pct sentinel-tone--${stripTone(status)}`}>{text}</span>
        <span className="llm-strip__chev">
          <Chevron />
        </span>
      </button>
      {open && (
        <div id={popoverId} role="group" aria-label="Sentinel" className="llm-pop">
          <SentinelDetails status={status} isAdmin={isAdmin} />
        </div>
      )}
    </div>
  );
}

/** Collapsed-rail stand-in for the strip: the shield alone, coloured by tone. */
export function SentinelRailIcon() {
  const { info } = useSentinel();
  const status = info?.status;
  if (!status?.enabled) return null;
  const label = `Sentinel: ${stripText(status)}`;
  return (
    <span role="img" aria-label={label} title={label} className={`sentinel-tone--${stripTone(status)} mt-2 inline-flex`}>
      <ShieldCheckIcon size={14} />
    </span>
  );
}
