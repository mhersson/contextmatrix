import { useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import type { Card } from '../../types';
import { chipTint, priorityColors, typeColors } from '../../lib/chip';
import { cardSignals, signalSvgProps } from '../../lib/cardSignals';

const HOVER_CARD_WIDTH = 240;
const GAP = 8;

const userIcon = (
  <svg {...signalSvgProps}>
    <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
    <circle cx="12" cy="7" r="4" />
  </svg>
);

const tagIcon = (
  <svg {...signalSvgProps}>
    <path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z" />
    <circle cx="7.5" cy="7.5" r=".5" fill="currentColor" />
  </svg>
);

interface CardHoverCardProps {
  card: Card;
  /** Element the panel floats beside; a null current skips positioning. */
  anchorRef: RefObject<HTMLElement | null>;
  /** DOM id the anchor references via aria-describedby. */
  id: string;
}

/**
 * Floating summary shown when a board card is hovered or focused: header
 * with id, type and priority; one row per signal (uncapped, so the "+N"
 * overflow becomes readable); HITL when the card is not autonomous; and the
 * card's labels as pills. Rendered through a portal so column scroll
 * containers cannot clip it.
 */
export function CardHoverCard({ card, anchorRef, id }: CardHoverCardProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);

  // Layout effect: the (0,0) first render is measured and moved before paint.
  useLayoutEffect(() => {
    const anchor = anchorRef.current;
    if (!anchor || !panelRef.current) return;
    const rect = anchor.getBoundingClientRect();
    const height = panelRef.current.offsetHeight;
    const fitsRight = rect.right + GAP + HOVER_CARD_WIDTH <= window.innerWidth;
    const left = fitsRight ? rect.right + GAP : Math.max(GAP, rect.left - GAP - HOVER_CARD_WIDTH);
    const top = Math.max(GAP, Math.min(rect.top, window.innerHeight - height - GAP));
    setPos({ top, left });
  }, [anchorRef]);

  const signals = cardSignals(card);
  const labels = card.labels ?? [];
  const mobPhases = card.mob_phases ?? [];

  const panel = (
    <div
      ref={panelRef}
      id={id}
      role="tooltip"
      className="card-hover"
      style={{
        width: HOVER_CARD_WIDTH,
        top: pos?.top ?? 0,
        left: pos?.left ?? 0,
      }}
    >
      <div className="card-hover__head" data-testid="hover-head">
        <span className="card-hover__id">{card.id}</span>
        <span className="chip-pill" style={chipTint(typeColors[card.type] || 'var(--grey1)')}>
          {card.type}
        </span>
        <span className="card-hover__priority">
          <span
            className="card-hover__dot"
            style={{ backgroundColor: priorityColors[card.priority] || 'var(--grey1)' }}
          />
          {card.priority}
        </span>
      </div>

      <ul className="card-hover__rows">
        {signals.map((s) => (
          <li key={s.key} className="card-hover__row">
            <span className="card-hover__icon" style={{ color: s.color }}>{s.icon}</span>
            <span className="card-hover__text">
              {s.label}
              {s.key === 'mob' && mobPhases.length > 0 && (
                <span className="card-hover__sub">{mobPhases.join(', ')}</span>
              )}
            </span>
          </li>
        ))}
        {!card.autonomous && (
          <li className="card-hover__row">
            <span className="card-hover__icon" style={{ color: 'var(--grey1)' }}>{userIcon}</span>
            <span className="card-hover__text">HITL - human in the loop</span>
          </li>
        )}
        {labels.length > 0 && (
          <li className="card-hover__row" data-testid="hover-labels">
            <span className="card-hover__icon" style={{ color: 'var(--grey1)' }}>{tagIcon}</span>
            <span className="card-hover__labels">
              {labels.map((label) => (
                <span key={label} className="chip-pill" style={chipTint('var(--grey1)')}>
                  {label}
                </span>
              ))}
            </span>
          </li>
        )}
      </ul>
    </div>
  );

  return createPortal(panel, document.body);
}
