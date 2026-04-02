import { useEffect, useRef, useCallback } from 'react';
import { useDraggable } from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import type { Card } from '../../types';
import { runnerStatusStyles } from '../../types';

interface CardItemProps {
  card: Card;
  onClick?: () => void;
  flashCardId?: string | null;
  isCollapsed?: boolean;
  onToggleCollapse?: (cardId: string) => void;
  onParentClick?: (cardId: string) => void;
}

const typeColors: Record<string, string> = {
  task: 'var(--blue)',
  bug: 'var(--red)',
  feature: 'var(--green)',
  subtask: 'var(--aqua)',
};

const priorityColors: Record<string, string> = {
  critical: 'var(--red)',
  high: 'var(--orange)',
  medium: 'var(--yellow)',
  low: 'var(--grey1)',
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
    >
      <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
          d={isCollapsed ? 'M19 9l-7 7-7-7' : 'M5 15l7-7 7 7'} />
      </svg>
    </button>
  ) : null;

  if (isCollapsed) {
    return (
      <div
        ref={setRefs}
        style={style}
        {...listeners}
        {...attributes}
        onClick={onClick}
        className={`
          bg-[var(--bg1)] rounded-md px-3 py-1.5 mb-2 cursor-grab active:cursor-grabbing
          transition-colors duration-150 hover:bg-[var(--bg2)]
          ${borderClass}
          ${isDragging ? 'shadow-lg z-50' : ''}
          ${isFlashing ? 'animate-card-flash' : ''}
        `}
      >
        {/* Collapsed header: ID, type badge, parent badge, and toggle button */}
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-[var(--grey1)] flex-shrink-0">{card.id}</span>
          <span
            className="text-xs px-1.5 py-0.5 rounded flex-shrink-0"
            style={{
              backgroundColor: `color-mix(in srgb, ${typeColors[card.type] || 'var(--grey1)'} 20%, transparent)`,
              color: typeColors[card.type] || 'var(--grey1)',
            }}
          >
            {card.type}
          </span>
          {card.parent && (
            <button
              onClick={(e) => { e.stopPropagation(); onParentClick?.(card.parent!); }}
              className="font-mono text-xs px-1.5 py-0.5 rounded flex-shrink-0 bg-[var(--bg-blue)] text-[var(--aqua)] hover:opacity-80 transition-opacity flex items-center gap-1"
              title={`Parent: ${card.parent}`}
              aria-label={`Navigate to parent ${card.parent}`}
            >
              <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
              </svg>
              {card.parent}
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
      className={`
        bg-[var(--bg1)] rounded-md p-3 mb-2 cursor-grab active:cursor-grabbing
        transition-colors duration-150 hover:bg-[var(--bg2)]
        ${borderClass}
        ${isDragging ? 'shadow-lg z-50' : ''}
        ${isFlashing ? 'animate-card-flash' : ''}
      `}
    >
      {/* Header: ID, Type badge, and collapse toggle */}
      <div className="flex items-center justify-between mb-2">
        <span className="font-mono text-xs text-[var(--grey1)]">{card.id}</span>
        <div className="flex items-center gap-1.5">
          <span
            className="text-xs px-1.5 py-0.5 rounded"
            style={{
              backgroundColor: `color-mix(in srgb, ${typeColors[card.type] || 'var(--grey1)'} 20%, transparent)`,
              color: typeColors[card.type] || 'var(--grey1)',
            }}
          >
            {card.type}
          </span>
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
        />

        {/* Parent ID badge */}
        {card.parent && (
          <button
            onClick={(e) => { e.stopPropagation(); onParentClick?.(card.parent!); }}
            className="font-mono text-xs px-1.5 py-0.5 rounded bg-[var(--bg-blue)] text-[var(--aqua)] hover:opacity-80 transition-opacity flex items-center gap-1"
            title={`Parent: ${card.parent}`}
            aria-label={`Navigate to parent ${card.parent}`}
          >
            <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
            </svg>
            {card.parent}
          </button>
        )}

        {/* Agent indicator */}
        {card.assigned_agent && (
          <span
            className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg-blue)] text-[var(--aqua)] truncate max-w-[120px]"
            title={card.assigned_agent}
          >
            {card.assigned_agent}
          </span>
        )}

        {/* Dependency status */}
        {card.depends_on && card.depends_on.length > 0 && (
          <span
            className={`text-xs px-1.5 py-0.5 rounded ${
              card.dependencies_met
                ? 'bg-[var(--bg-green)] text-[var(--green)]'
                : 'bg-[var(--bg-red)] text-[var(--red)]'
            }`}
            title={card.dependencies_met ? 'All dependencies met' : 'Blocked by dependencies'}
          >
            {card.dependencies_met ? 'deps met' : 'blocked'}
          </span>
        )}

        {/* Autonomous badge */}
        {card.autonomous && (
          <span
            className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg-purple)] text-[var(--purple)]"
            title="Autonomous mode"
          >
            auto
          </span>
        )}

        {/* Runner status badge */}
        {card.runner_status && runnerStatusStyles[card.runner_status] && (
          <span
            className={`text-xs px-1.5 py-0.5 rounded${card.runner_status === 'running' ? ' animate-pulse' : ''}`}
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
            className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg-green)] text-[var(--green)] font-mono truncate max-w-[120px]"
            title={`Branch: ${card.branch_name}`}
          >
            {card.branch_name.split('/').pop()}
          </span>
        )}

        {/* Labels */}
        {card.labels?.map((label) => (
          <span
            key={label}
            className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg-purple)] text-[var(--purple)]"
          >
            {label}
          </span>
        ))}
      </div>
    </div>
  );
}
