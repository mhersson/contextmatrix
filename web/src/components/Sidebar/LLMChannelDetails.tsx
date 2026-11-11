import { useEffect, useState } from 'react';
import {
  channelExceptions,
  channelLabel,
  formatReset,
  inFlightBreakdown,
  inFlightTotal,
  otherMode,
  rejectedWindow,
  usagePct,
  usageTone,
  windowLabel,
} from '../../lib/llmChannel';
import type { LLMChannel, LLMChannelUsage, LLMChannelView } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';

const RELATIVE_TICK_MS = 30_000;
const MODES: LLMChannel[] = ['endpoint', 'claude_code'];

function ArrowIcon() {
  return (
    <svg aria-hidden="true" width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <path d="M2 8h9M8 4l4 4-4 4" />
    </svg>
  );
}

function WarnIcon() {
  return (
    <svg aria-hidden="true" width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path d="M8 1.8 15 14H1z M8 6v4 M8 11.6v.4" />
    </svg>
  );
}

function StopIcon() {
  return (
    <svg aria-hidden="true" width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6">
      <circle cx="8" cy="8" r="6.2" />
      <path d="M5 8h6" />
    </svg>
  );
}

function LockIcon() {
  return (
    <svg role="img" aria-label="Locked" width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <rect x="3" y="7" width="10" height="7" rx="1" />
      <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" />
    </svg>
  );
}

function Meter({ usage }: { usage: LLMChannelUsage }) {
  const pct = usagePct(usage);
  const reset = formatReset(usage.resets_at);
  const meta = usage.stale
    ? `reset ${reset ?? 'time not reported'} · no reading since`
    : `${reset ? `resets ${reset}` : 'reset time not reported'} · seen ${formatRelativeTime(usage.seen_at)}`;
  return (
    <div className="llm-meter">
      <div className="llm-meter__top">
        <span>{windowLabel(usage.window)}</span>
        {!usage.stale && <span className="llm-meter__pct">{pct}%</span>}
      </div>
      <div className="llm-meter__bar" aria-hidden="true">
        <i
          className={`llm-meter__fill llm-meter__fill--${usageTone(usage)}`}
          style={{ width: `${usage.stale ? 0 : Math.min(Math.max(pct, 0), 100)}%` }}
        />
      </div>
      <div className="llm-meter__meta">{meta}</div>
    </div>
  );
}

interface LLMChannelDetailsProps {
  view: LLMChannelView;
  /** Both sides are configured and this viewer may switch. */
  canSwitch: boolean;
  onSwitch: (mode: LLMChannel) => Promise<void>;
  error: string | null;
}

/** Body of the sidebar's LLM channel popover: the switch or the active mode, live work, usage and exceptions. */
export function LLMChannelDetails({ view, canSwitch, onSwitch, error }: LLMChannelDetailsProps) {
  const [busy, setBusy] = useState(false);
  const [, setTick] = useState(0);

  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), RELATIVE_TICK_MS);
    return () => clearInterval(id);
  }, []);

  const switchTo = async (mode: LLMChannel) => {
    if (mode === view.mode || busy) return;
    setBusy(true);
    try {
      await onSwitch(mode);
    } finally {
      setBusy(false);
    }
  };

  const other = otherMode(view.mode);
  const otherCount = inFlightTotal(view, other);
  const rejected = rejectedWindow(view.usage);
  const endpointLabel = channelLabel(view, 'endpoint');

  return (
    <>
      {canSwitch ? (
        <div className="mk-seg llm-seg" role="group" aria-label="LLM channel mode">
          {MODES.map((m) => (
            <button key={m} type="button" aria-pressed={view.mode === m} disabled={busy} onClick={() => void switchTo(m)}>
              {channelLabel(view, m)}
            </button>
          ))}
        </div>
      ) : (
        <div className="llm-row" title={view.switchable ? 'Set by an admin' : 'No endpoint is configured'}>
          <span className={`llm-dot llm-dot--${view.mode}`} aria-hidden="true" />
          {view.mode === 'claude_code' ? 'Claude Code subscription' : endpointLabel}
          {view.switchable && (
            <span className="llm-row__lock">
              <LockIcon />
            </span>
          )}
        </div>
      )}
      {error && <p className="llm-error" role="alert">{error}</p>}

      {otherCount > 0 && (
        <div className="llm-line">
          <span className="llm-line__icon llm-line__icon--blue"><ArrowIcon /></span>
          <span>
            <b>{otherCount}</b> still on {channelLabel(view, other)}: {inFlightBreakdown(view, other)}
          </span>
        </div>
      )}

      {view.usage.map((u) => <Meter key={u.window} usage={u} />)}

      {rejected && (
        <div className="llm-line">
          <span className="llm-line__icon llm-line__icon--red"><StopIcon /></span>
          <span>Runs park until the reset; nothing falls back to {endpointLabel}</span>
        </div>
      )}

      {channelExceptions(view).map((e) => (
        <div key={e.text} className="llm-line">
          <span className="llm-line__icon llm-line__icon--yellow"><WarnIcon /></span>
          <span>{`${e.text}: ${e.reason}`}</span>
        </div>
      ))}
    </>
  );
}
