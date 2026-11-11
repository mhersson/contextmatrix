import { Fragment, useCallback, useEffect, useId, useRef, useState } from 'react';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useLLMChannel } from '../../hooks/useLLMChannel';
import {
  channelLabel,
  headlineWindow,
  subLineItems,
  subLineText,
  usagePct,
  usageTone,
  windowShort,
} from '../../lib/llmChannel';
import { LLMChannelDetails } from './LLMChannelDetails';

function Chevron() {
  return (
    <svg aria-hidden="true" width="9" height="9" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 6l5 5 5-5" />
    </svg>
  );
}

/**
 * Brand-header strip naming the active LLM channel, with a popover holding
 * the details and, for a viewer who may switch, the switch. Absent unless
 * Claude Code is configured. The popover is positioned against the nearest
 * positioned ancestor, so the brand header must be `relative`.
 */
export function LLMChannelStrip() {
  const { view, setMode, error, clearError } = useLLMChannel();
  const auth = useOptionalAuth();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const subId = useId();

  const close = useCallback(() => {
    setOpen(false);
    clearError();
  }, [clearError]);

  useEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      if (containerRef.current?.contains(e.target as Node)) return;
      // Focus would otherwise drop to <body> with the popover it sat in.
      if (containerRef.current?.contains(document.activeElement)) stripRef.current?.focus();
      close();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
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

  if (!view?.claude_code) return null;

  // UX only: the API 403s a non-admin switch in multi mode.
  const mayChange = auth?.mode !== 'multi' || Boolean(auth?.user?.is_admin);
  const canSwitch = view.switchable && mayChange;
  const label = channelLabel(view, view.mode);
  const headline = view.mode === 'claude_code' ? headlineWindow(view.usage) : null;
  const tone = headline ? usageTone(headline) : 'ok';
  const pct = headline && (tone === 'stop' ? 'limit' : `${usagePct(headline)}% ${windowShort(headline.window)}`);
  const items = subLineItems(view);

  return (
    <div ref={containerRef}>
      <button
        ref={stripRef}
        type="button"
        className={`llm-strip${canSwitch ? '' : ' llm-strip--ro'}`}
        aria-label={`LLM channel: ${label}${pct ? `, ${pct}` : ''}`}
        aria-expanded={open}
        aria-controls={popoverId}
        aria-describedby={items.length > 0 ? subId : undefined}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <span className={`llm-dot llm-dot--${view.mode}`} aria-hidden="true" />
        <span className="llm-strip__label">{label}</span>
        {pct && <span className={`llm-strip__pct llm-strip__pct--${tone}`}>{pct}</span>}
        <span className="llm-strip__chev">
          <Chevron />
        </span>
      </button>
      {items.length > 0 && (
        <p id={subId} className="llm-sub" title={items.map(subLineText).join(' · ')}>
          {items.map((item, i) => (
            <Fragment key={i}>
              {i > 0 && ' · '}
              {item.count !== undefined && <><b>{item.count}</b> </>}
              {item.text}
            </Fragment>
          ))}
        </p>
      )}
      {open && (
        <div id={popoverId} role="group" aria-label="LLM channel" className="llm-pop">
          <LLMChannelDetails view={view} canSwitch={canSwitch} onSwitch={setMode} error={error} />
        </div>
      )}
    </div>
  );
}

/** Collapsed-rail stand-in for the strip: a dot in the mode colour. */
export function LLMChannelRailDot() {
  const { view } = useLLMChannel();
  if (!view?.claude_code) return null;
  const label = `LLM channel: ${channelLabel(view, view.mode)}`;
  return <span role="img" aria-label={label} title={label} className={`llm-dot llm-rail-dot llm-dot--${view.mode}`} />;
}
