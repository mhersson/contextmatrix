import type { Card } from '../../types';
import { signalSvgProps } from '../../lib/cardSignals';
import { cardTiming, elapsedSeconds, isWorkedOn, shortDuration } from '../../lib/cardTiming';
import { displayState } from '../../lib/stateLabels';
import { useNow } from '../../hooks/useNow';

/** The tile's clock: the time in the current state while an agent works on the card. */
export function CardClock({ card }: { card: Card }) {
  const visible = isWorkedOn(card);
  const now = useNow(visible ? 30_000 : null);
  if (!visible) return null;

  return (
    <span
      data-testid="card-clock"
      className="inline-flex items-center gap-1 flex-shrink-0 whitespace-nowrap font-mono text-[11px] font-medium text-[var(--fg)]"
    >
      <svg {...signalSvgProps} width={12} height={12} className="text-[var(--aqua)]">
        <circle cx="12" cy="12" r="10" />
        <path d="M12 6v6l4 2" />
      </svg>
      <span className="sr-only">{displayState(card.state)} for </span>
      {shortDuration(elapsedSeconds(cardTiming(card).stateSince, now))}
    </span>
  );
}
