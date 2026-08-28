import type { Card } from '../../types';
import { chipTint } from '../../lib/chip';
import { cardSignals, splitCardSignals } from '../../lib/cardSignals';

/**
 * The board card's signal cluster: what used to be footer pills (auto, mob,
 * best-of-n, deps, worker status) plus playbook membership and the special
 * "simple" label, rendered as accent-tinted stroke icons with tooltip + aria
 * labels. Crowded cards cap at four icons; the least important collapse
 * behind a "+N" chip whose tooltip names them.
 */
export function CardSignalIcons({ card }: { card: Card }) {
  const signals = cardSignals(card);
  if (signals.length === 0) return null;

  const { visible, hidden } = splitCardSignals(signals);

  return (
    <span className="flex items-center gap-1.5 flex-shrink-0">
      {visible.map((s) => (
        <span
          key={s.key}
          className={`flex items-center flex-shrink-0${s.pulse ? ' animate-pulse motion-reduce:animate-none' : ''}`}
          style={{ color: s.color }}
          title={s.label}
          role="img"
          aria-label={s.label}
        >
          {s.icon}
        </span>
      ))}
      {hidden.length > 0 && (
        <span
          className="chip-pill flex-shrink-0"
          style={chipTint('var(--grey1)')}
          title={hidden.map((s) => s.label).join('\n')}
          role="img"
          aria-label={`${hidden.length} more signals: ${hidden.map((s) => s.label).join(', ')}`}
        >
          +{hidden.length}
        </span>
      )}
    </span>
  );
}
