import { useEffect, useMemo, useRef, useState } from 'react';
import type { Card } from '../../types';
import { SubtaskStrip, SubtaskPeekList } from './SubtaskStrip';

interface SpotlightStripProps {
  cards: Card[];
  onCardClick: (cardId: string) => void;
  /** Attached subtasks keyed by parent id; Board's memoized map. */
  subtasksByParent?: Map<string, Card[]>;
  flashCardId?: string | null;
}

interface SpotlightCardProps {
  card: Card;
  subtasks: Card[];
  onCardClick: (cardId: string) => void;
  flashCardId?: string | null;
}

/**
 * One surfaced card. The phase strip and peek rows are real buttons, so the
 * card cannot itself be a button or role="button" (ARIA buttons forbid
 * interactive descendants). Instead a plain div hosts a native "open" button
 * whose ::after stretches over the whole card; the strip/peek wrapper is
 * positioned above the stretch so its buttons stay clickable.
 */
function SpotlightCard({ card, subtasks, onCardClick, flashCardId }: SpotlightCardProps) {
  // Temporary peek: deliberately ephemeral component state, never persisted.
  // An explicit toggle wins; otherwise a flash targeting one of the subtasks
  // holds the peek open, since the flashed row renders nowhere else.
  const [peekChoice, setPeekChoice] = useState<boolean | null>(null);
  const stateLabel = card.state === 'stalled' ? 'stalled' : 'blocked';
  const hasSubtasks = subtasks.length > 0;
  const subtaskFlash = !!flashCardId && subtasks.some((s) => s.id === flashCardId);
  const isFlashing = card.id === flashCardId || subtaskFlash;
  const peekOpen = peekChoice ?? subtaskFlash;
  const cardRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (isFlashing && cardRef.current) {
      cardRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
  }, [isFlashing]);

  return (
    <div ref={cardRef} className={`spotlight-card${isFlashing ? ' animate-card-flash' : ''}`}>
      <button
        type="button"
        className="spotlight-card__open"
        onClick={() => onCardClick(card.id)}
        aria-label={`Open ${card.id} – ${stateLabel}`}
      >
        <span className="spotlight-card__id">{card.id}</span>
        <span className="spotlight-card__since">{stateLabel}</span>
        <span />
        <span className="spotlight-card__title">{card.title}</span>
      </button>
      {hasSubtasks && (
        <div className="spotlight-card__subtasks">
          <SubtaskStrip
            subtasks={subtasks}
            expanded={peekOpen}
            onToggle={() => setPeekChoice(!peekOpen)}
          />
          {peekOpen && (
            <div className="spotlight-card__peek">
              <SubtaskPeekList subtasks={subtasks} onOpen={onCardClick} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * Surfaces cards that need attention regardless of column position:
 *   - state == "stalled"
 *   - state == "blocked"
 *
 * Always rendered - when there is nothing to surface, the strip shows an
 * "all clear" placeholder so the slot in the layout remains visible.
 */
export function SpotlightStrip({ cards, onCardClick, subtasksByParent, flashCardId }: SpotlightStripProps) {
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
            <SpotlightCard
              key={c.id}
              card={c}
              subtasks={subtasksByParent?.get(c.id) ?? []}
              onCardClick={onCardClick}
              flashCardId={flashCardId}
            />
          ))}
        </div>
      )}
    </div>
  );
}
