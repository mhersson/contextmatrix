import type { ReactNode } from 'react';
import { signalIcons, signalSvgProps } from '../../lib/cardSignals';
import { duration, formatStamp, limitDuration, type TimingSummary } from '../../lib/cardTiming';

const timerIcon = (
  <svg {...signalSvgProps}>
    <path d="M10 2h4" />
    <path d="M12 14l3-3" />
    <circle cx="12" cy="14" r="8" />
  </svg>
);

const historyIcon = (
  <svg {...signalSvgProps}>
    <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
    <path d="M3 3v5h5" />
    <path d="M12 7v5l4 2" />
  </svg>
);

function Stamp({ ts }: { ts: string | null | undefined }) {
  const text = formatStamp(ts);
  return text && ts ? <time dateTime={ts}>{text}</time> : null;
}

function Row({ icon, tone = 'var(--grey1)', children, sub, testId }: {
  icon: ReactNode;
  tone?: string;
  children: ReactNode;
  sub?: ReactNode;
  testId?: string;
}) {
  return (
    <li className="card-hover__row" data-testid={testId}>
      <span className="card-hover__icon" style={{ color: tone }}>{icon}</span>
      <span className="card-hover__text">
        <span>{children}</span>
        {sub && <span className="card-hover__sub">{sub}</span>}
      </span>
    </li>
  );
}

interface CardTimingRowsProps {
  timing: TimingSummary;
  /** Show only the newest claims and say how many are left out (the tile's timing box). */
  maxIntervals?: number;
}

/**
 * The card's timing, one row per fact: the state and how long (or, on a
 * finished card, when), who holds it and since when or how long nobody has,
 * the heartbeat, the total worked time and the recorded claims. Shared by
 * the tile clock's timing box and the card's Info tab so both read the same.
 */
export function CardTimingRows({ timing: t, maxIntervals }: CardTimingRowsProps) {
  const shown = maxIntervals != null ? t.intervals.slice(-maxIntervals) : t.intervals;
  const hiddenCount = t.intervals.length - shown.length;

  return (
    <ul className="card-hover__rows" aria-label="Card timing">
      <Row
        icon={signalIcons.clock}
        testId="timing-state"
        sub={t.finished ? undefined : <>since <Stamp ts={t.stateSince} /></>}
      >
        {t.finished
          ? <>{t.state} <Stamp ts={t.stateSince} /></>
          : <>{t.state} for {duration(t.stateAge)}</>}
      </Row>

      {t.holder ? (
        <Row
          icon={signalIcons.bot}
          tone="var(--aqua)"
          testId="timing-holder"
          sub={t.workingSince ? <>since <Stamp ts={t.workingSince} /></> : 'claim start not recorded'}
        >
          {t.workingSince ? <>{t.holder} working for {duration(t.workingAge)}</> : <>{t.holder} holds it</>}
        </Row>
      ) : !t.finished && (
        <Row
          icon={signalIcons.hourglass}
          tone={t.warningSeconds != null ? 'var(--yellow)' : undefined}
          testId="timing-holder"
          sub={t.unclaimedSince ? <>for {duration(t.unclaimedAge)}, since <Stamp ts={t.unclaimedSince} /></> : undefined}
        >
          No agent holds it
        </Row>
      )}

      {t.holder && (
        <Row icon={signalIcons.activity} testId="timing-heartbeat">
          {t.heartbeatAge == null ? 'No heartbeat yet' : `Heartbeat ${duration(t.heartbeatAge)} ago`}
        </Row>
      )}

      <Row
        icon={timerIcon}
        testId="timing-worked"
        sub={t.worked == null ? 'no complete claim record' : 'held time, all claims'}
      >
        {t.worked == null ? 'Worked: unknown' : `Worked ${duration(t.worked)} in total`}
      </Row>

      {t.intervals.length > 0 && (
        <Row icon={historyIcon} testId="timing-intervals" sub={
          <span className="card-timing__claims">
            {shown.map((i, index) => (
              <span key={`${i.from}-${index}`}>
                <span className="card-timing__agent">{i.agent}</span>{' '}
                <Stamp ts={i.from} /> → {i.to ? <Stamp ts={i.to} /> : 'now'} · {duration(i.seconds)}
              </span>
            ))}
            {hiddenCount > 0 && <span>{hiddenCount} earlier in the card's Info tab</span>}
          </span>
        }>
          {t.intervals.length === 1 ? '1 claim' : `${t.intervals.length} claims`}
        </Row>
      )}

      {t.warningSeconds != null && !t.finished && (
        <Row icon={signalIcons.alert} tone="var(--yellow)" testId="timing-warning">
          No agent for over {limitDuration(t.warningSeconds)}
        </Row>
      )}
    </ul>
  );
}
