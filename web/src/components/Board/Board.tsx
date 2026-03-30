import { useMemo } from 'react';
import type { Card, ProjectConfig } from '../../types';
import { Column } from './Column';

interface BoardProps {
  cards: Card[];
  config: ProjectConfig;
  loading: boolean;
  error: string | null;
  onCardClick?: (card: Card) => void;
}

export function Board({ cards, config, loading, error, onCardClick }: BoardProps) {
  // Group cards by state
  const cardsByState = useMemo(() => {
    const grouped: Record<string, Card[]> = {};
    for (const state of config.states) {
      grouped[state] = [];
    }
    for (const card of cards) {
      if (grouped[card.state]) {
        grouped[card.state].push(card);
      }
    }
    return grouped;
  }, [cards, config.states]);

  if (loading) {
    return (
      <div className="flex gap-4 p-4">
        {[...Array(5)].map((_, i) => (
          <div
            key={i}
            className="w-[280px] min-w-[280px] h-[400px] bg-[var(--bg0)] rounded-lg border border-[var(--bg3)] animate-pulse"
          />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-6">
        <div className="bg-[var(--bg-red)] border border-[var(--red)] rounded-lg p-4">
          <p className="text-[var(--red)]">{error}</p>
        </div>
      </div>
    );
  }

  const totalCards = cards.length;

  return (
    <div className="flex flex-col h-full">
      {/* Board header */}
      <div className="px-4 py-3 border-b border-[var(--bg3)]">
        <h1 className="text-lg font-medium text-[var(--fg)]">{config.name}</h1>
        <p className="text-xs text-[var(--grey1)]">
          {totalCards} {totalCards === 1 ? 'card' : 'cards'}
        </p>
      </div>

      {/* Columns */}
      <div className="flex-1 overflow-x-auto overflow-y-hidden">
        <div className="flex gap-4 p-4 h-full min-w-max">
          {config.states.map((state) => (
            <Column
              key={state}
              state={state}
              cards={cardsByState[state]}
              config={config}
              onCardClick={onCardClick}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
