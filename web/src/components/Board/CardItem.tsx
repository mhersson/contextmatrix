import { memo, useEffect, useRef, useCallback } from 'react';
import { useDraggable } from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import type { Card } from '../../types';
import { chipTint, typeColors } from '../../lib/chip';
import { gitHubIcon } from '../icons';
import { CardChipRow } from './CardChipRow';

interface CardItemProps {
  card: Card;
  onClick?: () => void;
  flashCardId?: string | null;
  isCollapsed?: boolean;
  onToggleCollapse?: (cardId: string) => void;
  onParentClick?: (cardId: string) => void;
}

const cardIdStyle: React.CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontWeight: 500,
  fontSize: '11px',
  letterSpacing: '0.04em',
  color: 'var(--grey1)',
};

function CardItemImpl({ card, onClick, flashCardId, isCollapsed, onToggleCollapse, onParentClick }: CardItemProps) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: card.id,
    data: { card },
  });

  const cardRef = useRef<HTMLDivElement>(null);
  const isFlashing = card.id === flashCardId;

  const setRefs = useCallback((node: HTMLDivElement | null) => {
    setNodeRef(node);
    cardRef.current = node;
  }, [setNodeRef]);

  useEffect(() => {
    if (isFlashing && cardRef.current) {
      cardRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
  }, [isFlashing]);

  const style = {
    transform: CSS.Translate.toString(transform),
    opacity: isDragging ? 0.5 : 1,
  };

  const isAgentActive = card.assigned_agent && card.state !== 'stalled';
  const isStalled = card.state === 'stalled';
  const isNotPlanned = card.state === 'not_planned';

  const borderClass = isStalled
    ? 'border-l-[3px] border-l-[var(--red)]'
    : isNotPlanned
      ? 'border-l-[3px] border-l-[var(--bg4)]'
      : isAgentActive
        ? 'border-l-[3px] border-l-[var(--aqua)] animate-pulse-border'
        : 'border-l-[3px] border-l-transparent';

  const stalledBg: React.CSSProperties | undefined = isStalled ? {
    background: 'linear-gradient(90deg, color-mix(in oklab, var(--bg-red) 75%, transparent) 0%, var(--bg1) 50%)',
  } : undefined;

  const activeBg: React.CSSProperties | undefined = isAgentActive ? {
    background: 'linear-gradient(90deg, color-mix(in oklab, var(--bg-aqua) 60%, transparent) 0%, var(--bg1) 40%)',
  } : undefined;

  const collapseButton = onToggleCollapse ? (
    <button
      onClick={(e) => { e.stopPropagation(); onToggleCollapse(card.id); }}
      className="w-4 h-4 flex items-center justify-center rounded text-[var(--grey1)] hover:text-[var(--fg)] hover:bg-[var(--bg3)] transition-colors flex-shrink-0"
      title={isCollapsed ? 'Expand card' : 'Collapse card'}
      aria-label={isCollapsed ? 'Expand card' : 'Collapse card'}
      aria-expanded={!isCollapsed}
    >
      <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
          d={isCollapsed ? 'M19 9l-7 7-7-7' : 'M5 15l7-7 7 7'} />
      </svg>
    </button>
  ) : null;

  // Enter opens the card (matches onClick). Space is reserved for dnd-kit's
  // KeyboardSensor to pick up / drop during drag, so we must not swallow it.
  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      onClick?.();
    }
  };

  if (isCollapsed) {
    return (
      <div
        ref={setRefs}
        style={{ ...style, ...(stalledBg ?? activeBg) }}
        {...listeners}
        {...attributes}
        onClick={onClick}
        onKeyDown={handleKeyDown}
        aria-label={`Card ${card.id}: ${card.title}`}
        className={`
          bg-[var(--bg1)] rounded-[10px] px-3 py-1.5 mb-2 cursor-grab active:cursor-grabbing
          transition-all duration-150 hover:bg-[var(--bg2)] hover:-translate-y-px hover:shadow-[0_6px_18px_-8px_rgba(0,0,0,0.35)]
          focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)]
          ${borderClass}
          ${isDragging ? 'shadow-lg z-50' : ''}
          ${isFlashing ? 'animate-card-flash' : ''}
        `}
      >
        {/* Collapsed header: ID, type badge, parent badge, and toggle button */}
        <div className="flex items-center gap-2">
          <span className="flex-shrink-0" style={cardIdStyle}>{card.id}</span>
          <CardChipRow card={card} compact onParentClick={onParentClick} />
          <span className="text-xs text-[var(--fg)] truncate min-w-0 flex-1">{card.title}</span>
          {collapseButton}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={setRefs}
      style={{ ...style, ...(stalledBg ?? activeBg) }}
      {...listeners}
      {...attributes}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      aria-label={`Card ${card.id}: ${card.title}`}
      className={`
        bg-[var(--bg1)] rounded-[10px] p-3 mb-2 cursor-grab active:cursor-grabbing
        transition-all duration-150 hover:bg-[var(--bg2)] hover:-translate-y-px hover:shadow-[0_6px_18px_-8px_rgba(0,0,0,0.35)]
        focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)]
        ${borderClass}
        ${isDragging ? 'shadow-lg z-50' : ''}
        ${isFlashing ? 'animate-card-flash' : ''}
      `}
    >
      {/* Header: ID, Type badge, and collapse toggle */}
      <div className="flex items-center justify-between mb-2">
        <span style={cardIdStyle}>{card.id}</span>
        <div className="flex items-center gap-1.5">
          <span className="chip-pill" style={chipTint(typeColors[card.type] || 'var(--grey1)')}>
            {card.type}
          </span>
          {card.source?.system === 'github' && gitHubIcon}
          {card.source && !card.vetted && (
            <span className="chip-pill flex-shrink-0" style={chipTint('var(--yellow)')}>
              unvetted
            </span>
          )}
          {collapseButton}
        </div>
      </div>

      {/* Title */}
      <h3
        className={`mb-2 line-clamp-2 ${isNotPlanned ? 'text-[var(--grey1)]' : 'text-[var(--fg)]'}`}
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: '13.5px',
          fontWeight: 500,
          lineHeight: 1.32,
          letterSpacing: '-0.005em',
        }}
      >
        {card.title}
      </h3>

      {/* Footer: Priority, Parent, Agent, Labels */}
      <CardChipRow card={card} onParentClick={onParentClick} />
    </div>
  );
}

export const CardItem = memo(CardItemImpl);
export default CardItem;
