import { useOptionalAuth } from '../../hooks/useAuth';
import { usePopover } from '../../hooks/usePopover';
import { useSentinel } from '../../hooks/useSentinel';
import { stripText, stripTone } from '../../lib/sentinel';
import { ShieldCheckIcon } from '../ShieldCheckIcon';
import { Chevron } from './Chevron';
import { SentinelDetails } from './SentinelDetails';

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
  const { open, toggle, containerRef, triggerRef, popoverId, onBlur } = usePopover();

  const status = info?.status;
  if (!status?.enabled) return null;

  const text = stripText(status);
  return (
    <div
      ref={containerRef}
      onBlur={onBlur}
    >
      <button
        ref={triggerRef}
        type="button"
        className="llm-strip sentinel-strip"
        aria-label={`Sentinel: ${text}`}
        aria-expanded={open}
        aria-controls={popoverId}
        onClick={toggle}
      >
        <span className={`sentinel-tone--${stripTone(status)} inline-flex`} aria-hidden="true">
          <ShieldCheckIcon />
        </span>
        <span className="sentinel-strip__label">Sentinel</span>
        <span className={`sentinel-strip__status sentinel-tone--${stripTone(status)}`}>{text}</span>
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
