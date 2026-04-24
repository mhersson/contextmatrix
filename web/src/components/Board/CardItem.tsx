import { useEffect, useRef, useCallback } from 'react';
import { useDraggable } from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import type { Card } from '../../types';
import { runnerStatusStyles } from '../../types';
import { gitHubIcon } from '../icons';
import { chipTint, priorityColors, shortCardId, typeColors } from '../../lib/chip';

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

export function CardItem({ card, onClick, flashCardId, isCollapsed, onToggleCollapse, onParentClick }: CardItemProps) {
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
    ? 'border-l-[3px] border-l-[var(--red)] bg-[var(--bg-red)]'
    : isNotPlanned
      ? 'border-l-[3px] border-l-[var(--bg4)]'
      : isAgentActive
        ? 'border-l-[3px] border-l-[var(--aqua)] animate-pulse-border'
        : 'border-l-[3px] border-l-transparent';

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
        style={style}
        {...listeners}
        {...attributes}
        onClick={onClick}
        onKeyDown={handleKeyDown}
        aria-label={`Card ${card.id}: ${card.title}`}
        className={`
          bg-[var(--bg1)] rounded-md px-3 py-1.5 mb-2 cursor-grab active:cursor-grabbing
          transition-colors duration-150 hover:bg-[var(--bg2)]
          focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)]
          ${borderClass}
          ${isDragging ? 'shadow-lg z-50' : ''}
          ${isFlashing ? 'animate-card-flash' : ''}
        `}
      >
        {/* Collapsed header: ID, type badge, parent badge, and toggle button */}
        <div className="flex items-center gap-2">
          <span className="flex-shrink-0" style={cardIdStyle}>{card.id}</span>
          {card.type !== 'subtask' && (
            <span
              className="chip-pill flex-shrink-0"
              style={chipTint(typeColors[card.type] || 'var(--grey1)')}
              title={card.type}
              aria-label={`Type: ${card.type}`}
            >
              {card.type.charAt(0)}
            </span>
          )}
          {card.source?.system === 'github' && gitHubIcon}
          {card.source && !card.vetted && (
            <span className="chip-pill flex-shrink-0" style={chipTint('var(--yellow)')}>
              unvetted
            </span>
          )}
          {card.parent && (
            <button
              onClick={(e) => { e.stopPropagation(); onParentClick?.(card.parent!); }}
              className="chip-pill flex-shrink-0 hover:opacity-80 transition-opacity"
              style={{ background: 'var(--bg-blue)', color: 'var(--aqua)' }}
              title={`Parent: ${card.parent}`}
              aria-label={`Navigate to parent ${card.parent}`}
            >
              <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
              </svg>
              <span style={{ fontFamily: 'var(--font-mono)' }}>{shortCardId(card.parent)}</span>
            </button>
          )}
          <span className="text-xs text-[var(--fg)] truncate min-w-0 flex-1">{card.title}</span>
          {collapseButton}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={setRefs}
      style={style}
      {...listeners}
      {...attributes}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      aria-label={`Card ${card.id}: ${card.title}`}
      className={`
        bg-[var(--bg1)] rounded-md p-3 mb-2 cursor-grab active:cursor-grabbing
        transition-colors duration-150 hover:bg-[var(--bg2)]
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
      <h3 className={`text-sm font-medium mb-2 line-clamp-2 ${isNotPlanned ? 'text-[var(--grey1)]' : 'text-[var(--fg)]'}`}>
        {card.title}
      </h3>

      {/* Footer: Priority, Parent, Agent, Labels */}
      <div className="flex items-center flex-wrap gap-2">
        {/* Priority dot */}
        <span
          className="w-2 h-2 rounded-full"
          style={{ backgroundColor: priorityColors[card.priority] || 'var(--grey1)' }}
          title={card.priority}
          aria-label={`Priority: ${card.priority}`}
        />

        {/* Parent ID badge */}
        {card.parent && (
          <button
            onClick={(e) => { e.stopPropagation(); onParentClick?.(card.parent!); }}
            className="chip-pill hover:opacity-80 transition-opacity"
            style={chipTint('var(--aqua)')}
            title={`Parent: ${card.parent}`}
            aria-label={`Navigate to parent ${card.parent}`}
          >
            <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
            </svg>
            <span style={{ fontFamily: 'var(--font-mono)' }}>{shortCardId(card.parent)}</span>
          </button>
        )}

        {/* Agent indicator */}
        {card.assigned_agent && (
          <span
            className="chip-pill truncate max-w-[120px]"
            style={chipTint('var(--aqua)')}
            title={card.assigned_agent}
          >
            {card.assigned_agent}
          </span>
        )}

        {/* Dependency status */}
        {card.depends_on && card.depends_on.length > 0 && (
          <span
            className="chip-pill"
            style={chipTint(card.dependencies_met ? 'var(--green)' : 'var(--red)')}
            title={card.dependencies_met ? 'All dependencies met' : 'Blocked by dependencies'}
          >
            {card.dependencies_met ? 'deps met' : 'blocked'}
          </span>
        )}

        {/* Autonomous badge */}
        {card.autonomous && (
          <span
            className="chip-pill"
            style={chipTint('var(--purple)')}
            title="Autonomous mode"
          >
            auto
          </span>
        )}

        {/* Runner status badge */}
        {card.runner_status && runnerStatusStyles[card.runner_status] && (
          <span
            className={`chip-pill${card.runner_status === 'running' ? ' animate-pulse' : ''}`}
            style={{
              backgroundColor: runnerStatusStyles[card.runner_status].bg,
              color: runnerStatusStyles[card.runner_status].text,
            }}
            title={`Runner: ${card.runner_status}`}
            aria-label={`Runner status: ${card.runner_status}`}
          >
            {card.runner_status}
          </span>
        )}

        {/* Branch badge */}
        {card.branch_name && (
          <span
            className="chip-pill truncate max-w-[120px]"
            style={chipTint('var(--green)')}
            title={`Branch: ${card.branch_name}`}
          >
            {card.branch_name.split('/').pop()}
          </span>
        )}

        {/* Labels */}
        {card.labels?.map((label) => (
          <span key={label} className="chip-pill" style={chipTint('var(--purple)')}>
            {label}
          </span>
        ))}
      </div>
    </div>
  );
}
