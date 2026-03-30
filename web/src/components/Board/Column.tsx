import type { Card, ProjectConfig } from '../../types';
import { CardItem } from './CardItem';

interface ColumnProps {
  state: string;
  cards: Card[];
  config: ProjectConfig;
  onCardClick?: (card: Card) => void;
}

function formatStateName(state: string): string {
  return state
    .split('_')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

export function Column({ state, cards, onCardClick }: ColumnProps) {
  return (
    <div className="w-[280px] min-w-[280px] flex-shrink-0 flex flex-col bg-[var(--bg0)] rounded-lg border border-[var(--bg3)]">
      {/* Column header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-[var(--bg3)]">
        <h2 className="text-sm font-medium text-[var(--grey2)]">
          {formatStateName(state)}
        </h2>
        <span className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg2)] text-[var(--grey1)]">
          {cards.length}
        </span>
      </div>

      {/* Card list */}
      <div className="flex-1 p-2 overflow-y-auto min-h-0">
        {cards.length === 0 ? (
          <p className="text-xs text-[var(--grey0)] text-center py-4">
            No cards
          </p>
        ) : (
          cards.map((card) => (
            <CardItem
              key={card.id}
              card={card}
              onClick={() => onCardClick?.(card)}
            />
          ))
        )}
      </div>
    </div>
  );
}
