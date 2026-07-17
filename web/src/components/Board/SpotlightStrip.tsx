import { useMemo } from 'react';
import type { Card } from '../../types';

interface SpotlightStripProps {
  cards: Card[];
  onCardClick: (cardId: string) => void;
}

/**
 * Surfaces cards that need attention regardless of column position:
 *   - state == "stalled"
 *   - state == "blocked"
 *
 * Always rendered - when there is nothing to surface, the strip shows an
 * "all clear" placeholder so the slot in the layout remains visible.
 */
export function SpotlightStrip({ cards, onCardClick }: SpotlightStripProps) {
  const surfaced = useMemo(
    () => cards.filter((c) => c.state === 'stalled' || c.state === 'blocked'),
    [cards]
  );

  const stalledCount = surfaced.filter((c) => c.state === 'stalled').length;
  const blockedCount = surfaced.filter((c) => c.state === 'blocked').length;
  const empty = surfaced.length === 0;

  return (
    <div className="spotlight-strip" data-empty={empty ? 'true' : 'false'}>
      <div className="spotlight-strip__head">
        <span className="spotlight-strip__label">Needs Attention</span>
        <span className="spotlight-strip__meta">
          {empty
            ? 'all clear · auto-surfaced'
            : `${stalledCount} stalled · ${blockedCount} blocked · auto-surfaced`}
        </span>
      </div>
      {empty ? (
        <div className="spotlight-strip__empty">No stalled or blocked cards.</div>
      ) : (
        <div className="spotlight-strip__list">
          {surfaced.map((c) => (
            <button
              type="button"
              key={c.id}
              className="spotlight-card"
              onClick={() => onCardClick(c.id)}
              aria-label={`Open ${c.id} – ${c.state === 'stalled' ? 'stalled' : 'blocked'}`}
            >
              <span className="spotlight-card__id">{c.id}</span>
              <span className="spotlight-card__since">
                {c.state === 'stalled' ? 'stalled' : 'blocked'}
              </span>
              <span />
              <span className="spotlight-card__title">{c.title}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
