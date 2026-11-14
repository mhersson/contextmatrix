import { useOptionalAuth } from '../../hooks/useAuth';
import { usePopover } from '../../hooks/usePopover';
import { useSentinel } from '../../hooks/useSentinel';
import { chipText, stripText, stripTone } from '../../lib/sentinel';
import { ShieldCheckIcon } from '../ShieldCheckIcon';
import { SentinelDetails } from './SentinelDetails';

/** Admin in multi mode, anyone in none mode. UX only: the settings page is admin-gated server-side. */
function useIsAdmin(): boolean {
  const auth = useOptionalAuth();
  return auth?.mode !== 'multi' || Boolean(auth?.user?.is_admin);
}

/**
 * Status-row chip for Sentinel with a popover of details, shown only while it
 * is enabled. The popover is positioned against the nearest positioned
 * ancestor, so the brand header must stay `relative`.
 */
export function SentinelChip() {
  const { info } = useSentinel();
  const isAdmin = useIsAdmin();
  const { open, toggle, containerRef, triggerRef, popoverId, onBlur } = usePopover();

  const status = info?.status;
  if (!status?.enabled) return null;

  const full = stripText(status);
  const tone = stripTone(status);
  return (
    <div ref={containerRef} onBlur={onBlur} className="min-w-0">
      <button
        ref={triggerRef}
        type="button"
        className="sb-chip sb-chip--sentinel"
        title={full}
        aria-label={`Sentinel: ${full}`}
        aria-expanded={open}
        aria-controls={popoverId}
        onClick={toggle}
      >
        <span className={`sentinel-tone--${tone} inline-flex`} aria-hidden="true">
          <ShieldCheckIcon />
        </span>
        <span className={`sb-chip__label sentinel-tone--${tone}`}>{chipText(status)}</span>
      </button>
      {open && (
        <div id={popoverId} role="group" aria-label="Sentinel" className="llm-pop">
          <SentinelDetails status={status} isAdmin={isAdmin} />
        </div>
      )}
    </div>
  );
}

/** Collapsed-rail stand-in for the chip: the shield alone, coloured by tone. */
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
