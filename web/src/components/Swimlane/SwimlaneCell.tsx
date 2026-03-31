import type { Card } from '../../types';
import { SwimlaneCard } from './SwimlaneCard';

interface SwimlaneCellProps {
  cards: Card[];
}

export function SwimlaneCell({ cards }: SwimlaneCellProps) {
  return (
    <div
      className="p-1.5 rounded min-h-[60px] min-w-[160px]"
      style={{ backgroundColor: 'var(--bg0)' }}
    >
      {cards.length > 0 && (
        <div className="text-xs mb-1 px-0.5" style={{ color: 'var(--grey0)' }}>
          {cards.length}
        </div>
      )}
      <div className="space-y-1 max-h-[200px] overflow-y-auto">
        {cards.map((card) => (
          <SwimlaneCard key={card.id} card={card} />
        ))}
      </div>
    </div>
  );
}
