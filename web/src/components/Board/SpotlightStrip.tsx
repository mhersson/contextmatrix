import { useMemo } from 'react';
import type { Card } from '../../types';

interface SpotlightStripProps {
  cards: Card[];
  onCardClick: (cardId: string) => void;
}

/**
 * Surfaces cards that need attention regardless of column position:
 *   - state == "stalled"
 *   - depends_on.length > 0 && dependencies_met === false
 *
 * Hidden entirely when there are no such cards.
 */
export function SpotlightStrip({ cards, onCardClick }: SpotlightStripProps) {
  const surfaced = useMemo(
    () =>
      cards.filter(
        (c) =>
          c.state === 'stalled' ||
          (c.depends_on && c.depends_on.length > 0 && c.dependencies_met === false)
      ),
    [cards]
  );

  if (surfaced.length === 0) return null;

  const stalledCount = surfaced.filter((c) => c.state === 'stalled').length;
  const blockedCount = surfaced.length - stalledCount;

  return (
    <div className="spotlight-strip">
      <div className="spotlight-strip__head">
        <span className="spotlight-strip__label">Needs Attention</span>
        <span className="spotlight-strip__meta">
          {stalledCount} stalled · {blockedCount} blocked dep · auto-surfaced
        </span>
      </div>
      <div className="spotlight-strip__list">
        {surfaced.map((c) => (
          <button
            type="button"
            key={c.id}
            className="spotlight-card"
            onClick={() => onCardClick(c.id)}
            aria-label={`Open ${c.id}`}
          >
            <span className="spotlight-card__id">{c.id}</span>
            <span className="spotlight-card__since">
              {c.state === 'stalled' ? 'stalled' : 'blocked dep'}
            </span>
            <span />
            <span className="spotlight-card__title">{c.title}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
