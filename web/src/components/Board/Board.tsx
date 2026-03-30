import { useMemo, useState } from 'react';
import {
  DndContext,
  DragOverlay,
  closestCorners,
  useSensor,
  useSensors,
  PointerSensor,
  type DragStartEvent,
  type DragEndEvent,
} from '@dnd-kit/core';
import type { Card, ProjectConfig } from '../../types';
import { Column } from './Column';
import { CardItem } from './CardItem';

interface BoardProps {
  cards: Card[];
  config: ProjectConfig;
  loading: boolean;
  error: string | null;
  onCardClick?: (card: Card) => void;
  onCardMove?: (cardId: string, newState: string) => Promise<void>;
  onCreateCard?: (state: string) => void;
  flashCardId?: string | null;
}

export function Board({ cards, config, loading, error, onCardClick, onCardMove, onCreateCard, flashCardId }: BoardProps) {
  const [activeCard, setActiveCard] = useState<Card | null>(null);

  // Sensor with activation constraint to prevent accidental drags
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 5,
      },
    })
  );

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

  function handleDragStart(event: DragStartEvent) {
    const card = event.active.data.current?.card as Card | undefined;
    if (card) {
      setActiveCard(card);
    }
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveCard(null);

    if (!over || !onCardMove) return;

    const cardId = active.id as string;
    const newState = over.id as string;
    const card = active.data.current?.card as Card | undefined;

    if (!card || card.state === newState) return;

    // Validate transition
    const validTargets = config.transitions[card.state] || [];
    if (!validTargets.includes(newState)) {
      return; // Board parent (App) will show toast for invalid transition
    }

    onCardMove(cardId, newState);
  }

  function handleDragCancel() {
    setActiveCard(null);
  }

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
      <div className="px-4 py-3 border-b border-[var(--bg3)] flex items-center justify-between">
        <div>
          <h1 className="text-lg font-medium text-[var(--fg)]">{config.name}</h1>
          <p className="text-xs text-[var(--grey1)]">
            {totalCards} {totalCards === 1 ? 'card' : 'cards'}
          </p>
        </div>
        {onCreateCard && (
          <button
            onClick={() => onCreateCard(config.states[0])}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90 transition-opacity"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            New Card
          </button>
        )}
      </div>

      {/* Columns */}
      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        onDragCancel={handleDragCancel}
      >
        <div className="flex-1 overflow-x-auto overflow-y-hidden">
          <div className="flex gap-4 p-4 h-full min-w-max">
            {config.states.map((state) => (
              <Column
                key={state}
                state={state}
                cards={cardsByState[state]}
                config={config}
                onCardClick={onCardClick}
                onCreateCard={onCreateCard}
                activeCardState={activeCard?.state}
                flashCardId={flashCardId}
              />
            ))}
          </div>
        </div>

        <DragOverlay>
          {activeCard ? (
            <div className="w-[260px]">
              <CardItem card={activeCard} />
            </div>
          ) : null}
        </DragOverlay>
      </DndContext>
    </div>
  );
}
