import { useOptionalAuth } from '../../hooks/useAuth';
import { usePopover } from '../../hooks/usePopover';
import { useLLMChannel, type LLMChannelState } from '../../hooks/useLLMChannel';
import { channelLabel, chipTone, headlineWindow, usagePct, usageTone, windowShort } from '../../lib/llmChannel';
import { LLMChannelDetails } from './LLMChannelDetails';

/**
 * Status-row chip naming the active LLM channel, with a popover holding the
 * details and, for a viewer who may switch, the switch. Absent unless Claude
 * Code is configured. The popover is positioned against the nearest
 * positioned ancestor, so the brand header must be `relative`.
 */
export function LLMChannelChip({ state }: { state: LLMChannelState }) {
  const { view, setMode, error, clearError } = state;
  const auth = useOptionalAuth();
  const { open, toggle, containerRef, triggerRef, popoverId, onBlur } = usePopover(clearError);

  if (!view?.claude_code) return null;

  // UX only: the API 403s a non-admin switch in multi mode.
  const mayChange = auth?.mode !== 'multi' || Boolean(auth?.user?.is_admin);
  const canSwitch = view.switchable && mayChange;
  const label = channelLabel(view, view.mode);
  const tone = chipTone(view);
  const headline = view.mode === 'claude_code' ? headlineWindow(view.usage) : null;
  const pct = headline && (usageTone(headline) === 'stop' ? 'limit' : `${usagePct(headline)}% ${windowShort(headline.window)}`);

  return (
    <div ref={containerRef} onBlur={onBlur} className="min-w-0">
      <button
        ref={triggerRef}
        type="button"
        className={`sb-chip sb-chip--${tone}${canSwitch ? '' : ' sb-chip--ro'}`}
        aria-label={`LLM channel: ${label}${pct ? `, ${pct}` : ''}`}
        aria-expanded={open}
        aria-controls={popoverId}
        onClick={toggle}
      >
        <span className={`llm-dot llm-dot--${view.mode}`} aria-hidden="true" />
        <span className="sb-chip__label">{label}</span>
      </button>
      {open && (
        <div id={popoverId} role="group" aria-label="LLM channel" className="llm-pop">
          <LLMChannelDetails view={view} canSwitch={canSwitch} onSwitch={setMode} error={error} />
        </div>
      )}
    </div>
  );
}

/** Collapsed-rail stand-in for the chip: a dot in the mode colour. */
export function LLMChannelRailDot() {
  const { view } = useLLMChannel();
  if (!view?.claude_code) return null;
  const label = `LLM channel: ${channelLabel(view, view.mode)}`;
  return <span role="img" aria-label={label} title={label} className={`llm-dot llm-rail-dot llm-dot--${view.mode}`} />;
}
