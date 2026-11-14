import type { Card } from '../../types';
import { chipTint } from '../../lib/chip';
import { useSentinel } from '../../hooks/useSentinel';
import { cardSignals, splitCardSignals } from '../../lib/cardSignals';

/**
 * Header signal cluster: worker status, deps, autonomous, mob, best-of-n,
 * playbook membership and the "simple" label as tinted stroke icons with
 * aria labels. At most four show; the rest fold into a "+N" chip. No native
 * title: the browser holds those for ~1s, so the card's hovercard (opened
 * from the header group this cluster sits in) explains the icons instead.
 */
export function CardSignalIcons({ card }: { card: Card }) {
  const sentinelOn = useSentinel().info?.status.enabled ?? false;
  const signals = cardSignals(card, sentinelOn);
  if (signals.length === 0) return null;

  const { visible, hidden } = splitCardSignals(signals);

  return (
    <span className="flex items-center gap-1.5 flex-shrink-0">
      {visible.map((s) => (
        <span
          key={s.key}
          className={`flex items-center flex-shrink-0${s.pulse ? ' animate-pulse motion-reduce:animate-none' : ''}${s.working ? ' sentinel-working' : ''}`}
          style={{ color: s.color }}
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
          role="img"
          aria-label={`${hidden.length} more signals: ${hidden.map((s) => s.label).join(', ')}`}
        >
          +{hidden.length}
        </span>
      )}
    </span>
  );
}
