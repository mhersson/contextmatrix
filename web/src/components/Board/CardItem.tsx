import { useDraggable } from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import type { Card } from '../../types';

interface CardItemProps {
  card: Card;
  onClick?: () => void;
}

const typeColors: Record<string, string> = {
  task: 'var(--blue)',
  bug: 'var(--red)',
  feature: 'var(--green)',
};

const priorityColors: Record<string, string> = {
  critical: 'var(--red)',
  high: 'var(--orange)',
  medium: 'var(--yellow)',
  low: 'var(--grey1)',
};

export function CardItem({ card, onClick }: CardItemProps) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: card.id,
    data: { card },
  });

  const style = {
    transform: CSS.Translate.toString(transform),
    opacity: isDragging ? 0.5 : 1,
  };

  const isAgentActive = card.assigned_agent && card.state !== 'stalled';
  const isStalled = card.state === 'stalled';

  const borderClass = isStalled
    ? 'border-l-[3px] border-l-[var(--red)] bg-[var(--bg-red)]'
    : isAgentActive
      ? 'border-l-[3px] border-l-[var(--aqua)] animate-pulse-border'
      : 'border-l-[3px] border-l-transparent';

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...listeners}
      {...attributes}
      onClick={onClick}
      className={`
        bg-[var(--bg1)] rounded-md p-3 mb-2 cursor-grab active:cursor-grabbing
        transition-colors duration-150 hover:bg-[var(--bg2)]
        ${borderClass}
        ${isDragging ? 'shadow-lg z-50' : ''}
      `}
    >
      {/* Header: ID and Type badge */}
      <div className="flex items-center justify-between mb-2">
        <span className="font-mono text-xs text-[var(--grey1)]">{card.id}</span>
        <span
          className="text-xs px-1.5 py-0.5 rounded"
          style={{
            backgroundColor: `color-mix(in srgb, ${typeColors[card.type] || 'var(--grey1)'} 20%, transparent)`,
            color: typeColors[card.type] || 'var(--grey1)',
          }}
        >
          {card.type}
        </span>
      </div>

      {/* Title */}
      <h3 className="text-sm text-[var(--fg)] font-medium mb-2 line-clamp-2">
        {card.title}
      </h3>

      {/* Footer: Priority, Agent, Labels */}
      <div className="flex items-center flex-wrap gap-2">
        {/* Priority dot */}
        <span
          className="w-2 h-2 rounded-full"
          style={{ backgroundColor: priorityColors[card.priority] || 'var(--grey1)' }}
          title={card.priority}
        />

        {/* Agent indicator */}
        {card.assigned_agent && (
          <span
            className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg-blue)] text-[var(--aqua)] truncate max-w-[120px]"
            title={card.assigned_agent}
          >
            {card.assigned_agent}
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
