import { useMemo, useRef, useState } from 'react';
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
import type { Card, CardFilter, ProjectConfig } from '../../types';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useCollapsedColumns } from '../../hooks/useCollapsedColumns';
import { useCollapsedCards } from '../../hooks/useCollapsedCards';
import { Column } from './Column';
import { CardItem } from './CardItem';
import { FilterBar } from './FilterBar';
import { BoardSkeleton } from './BoardSkeleton';

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
  const [filter, setFilter] = useState<CardFilter>({});
  const filterBarRef = useRef<HTMLDivElement>(null);
  const cardIds = useMemo(() => cards.map((c) => c.id), [cards]);
  const [collapsedColumns, toggleCollapse] = useCollapsedColumns(config.name, config.states);
  const [collapsedCards, toggleCardCollapse] = useCollapsedCards(config.name, cardIds);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    })
  );

  const hasFilter = Object.values(filter).some(Boolean);

  const filteredCards = useMemo(() => {
    if (!hasFilter) return cards;
    return cards.filter((card) => {
      if (filter.type && card.type !== filter.type) return false;
      if (filter.priority && card.priority !== filter.priority) return false;
      if (filter.label && !(card.labels ?? []).includes(filter.label)) return false;
      if (filter.agent && card.assigned_agent !== filter.agent) return false;
      return true;
    });
  }, [cards, filter, hasFilter]);

  const cardsByState = useMemo(() => {
    const grouped: Record<string, Card[]> = {};
    for (const state of config.states) {
      grouped[state] = [];
    }
    for (const card of filteredCards) {
      if (grouped[card.state]) {
        grouped[card.state].push(card);
      }
    }
    return grouped;
  }, [filteredCards, config.states]);

  useKeyboardShortcuts(
    useMemo(
      () => [
        {
          key: '/',
          handler: () => filterBarRef.current?.querySelector('select')?.focus(),
        },
        {
          key: 'Escape',
          handler: () => {
            if (hasFilter) setFilter({});
          },
        },
      ],
      [hasFilter]
    )
  );

  function handleDragStart(event: DragStartEvent) {
    const card = event.active.data.current?.card as Card | undefined;
    if (card) setActiveCard(card);
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveCard(null);

    if (!over || !onCardMove) return;

    const cardId = active.id as string;
    const newState = over.id as string;
    const card = active.data.current?.card as Card | undefined;

    if (!card || card.state === newState) return;

    const validTargets = config.transitions[card.state] || [];
    if (!validTargets.includes(newState)) return;

    onCardMove(cardId, newState);
  }

  function handleDragCancel() {
    setActiveCard(null);
  }

  if (loading) return <BoardSkeleton />;

  if (error) {
    return (
      <div className="p-6">
        <div className="bg-[var(--bg-red)] border border-[var(--red)] rounded-lg p-4">
          <p className="text-[var(--red)]">{error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Board header */}
      <div className="px-4 py-3 border-b border-[var(--bg3)] flex items-center justify-between">
        <div>
          <h1 className="text-lg font-medium text-[var(--fg)]">{config.name}</h1>
          <p className="text-xs text-[var(--grey1)]">
            {hasFilter
              ? `${filteredCards.length} of ${cards.length} cards`
              : `${cards.length} ${cards.length === 1 ? 'card' : 'cards'}`}
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

      {/* Filter bar */}
      <FilterBar
        ref={filterBarRef}
        config={config}
        cards={cards}
        filter={filter}
        onFilterChange={setFilter}
      />

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
                collapsed={collapsedColumns.has(state)}
                onToggleCollapse={toggleCollapse}
                onCardClick={onCardClick}
                onCreateCard={onCreateCard}
                activeCardState={activeCard?.state}
                flashCardId={flashCardId}
                collapsedCards={collapsedCards}
                onToggleCardCollapse={toggleCardCollapse}
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
