import type { Card } from '../../../types';
import { isTerminalState } from '../../../lib/cardState';
import { cardTiming, duration, elapsedSeconds, formatStamp } from '../../../lib/cardTiming';
import { displayState } from '../../../lib/stateLabels';
import { useNow } from '../../../hooks/useNow';

function Stamp({ ts }: { ts: string }) {
  return <time dateTime={ts}>{formatStamp(ts)}</time>;
}

/**
 * Timing section of the Info rail tab: the state and how long (on a finished
 * card, when), then every recorded claim with the total. Derived from the
 * activity log.
 */
export function MetadataTiming({ card }: { card: Card }) {
  const { stateSince, intervals } = cardTiming(card);
  const finished = isTerminalState(card.state);
  const held = intervals.some((i) => i.to === null);
  const now = useNow(held || !finished ? 30_000 : null);
  const seconds = intervals.map((i) => elapsedSeconds(i.from, i.to ? Date.parse(i.to) : now));
  const state = displayState(card.state);

  return (
    <section className="bf-aside-section text-[12px] text-[var(--fg)]">
      <h4>Timing</h4>
      <div data-testid="timing-state">
        {finished ? (
          <>{state} <Stamp ts={stateSince} /></>
        ) : (
          <>
            {state} for {duration(elapsedSeconds(stateSince, now))}
            <div className="font-mono text-[11px] text-[var(--grey1)]">since <Stamp ts={stateSince} /></div>
          </>
        )}
      </div>
      {intervals.length > 0 && (
        <div data-testid="timing-claims" className="mt-3">
          {intervals.length === 1 ? '1 claim' : `${intervals.length} claims`} ·{' '}
          {duration(seconds.reduce((sum, s) => sum + s, 0))}
          <ul className="mt-1 flex flex-col gap-0.5 font-mono text-[11px] text-[var(--grey1)]">
            {intervals.map((i, n) => (
              <li key={`${i.from}-${n}`}>
                <span className="text-[var(--fg)]">{i.agent}</span> <Stamp ts={i.from} /> →{' '}
                {i.to ? <Stamp ts={i.to} /> : 'now'} · {duration(seconds[n])}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
