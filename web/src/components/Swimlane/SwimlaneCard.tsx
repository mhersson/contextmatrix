import { useNavigate } from 'react-router-dom';
import type { Card } from '../../types';

const PRIORITY_COLORS: Record<string, string> = {
  critical: 'var(--red)',
  high: 'var(--orange)',
  medium: 'var(--yellow)',
  low: 'var(--grey1)',
};

interface SwimlaneCardProps {
  card: Card;
}

export function SwimlaneCard({ card }: SwimlaneCardProps) {
  const navigate = useNavigate();

  return (
    <div
      className="px-2 py-1.5 rounded cursor-pointer transition-colors hover:brightness-110"
      style={{ backgroundColor: 'var(--bg1)' }}
      onClick={() => navigate(`/projects/${card.project}`)}
      title={card.title}
    >
      <div className="flex items-center gap-1.5">
        <span
          className="w-1.5 h-1.5 rounded-full shrink-0"
          style={{ backgroundColor: PRIORITY_COLORS[card.priority] || 'var(--grey1)' }}
        />
        <span
          className="text-xs truncate"
          style={{ color: 'var(--grey1)', fontFamily: 'var(--font-mono)' }}
        >
          {card.id}
        </span>
        {card.assigned_agent && (
          <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: 'var(--aqua)' }} />
        )}
      </div>
      <div className="text-xs mt-0.5 truncate" style={{ color: 'var(--fg)' }}>
        {card.title}
      </div>
    </div>
  );
}
