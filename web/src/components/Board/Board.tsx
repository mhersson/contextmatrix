import { useMemo, useState } from 'react';
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  closestCorners,
  useSensor,
  useSensors,
  PointerSensor,
  TouchSensor,
  type DragStartEvent,
  type DragEndEvent,
} from '@dnd-kit/core';
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import type { ActiveAgent, Card, CardFilter, MetricSeries, ProjectConfig } from '../../types';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { useCollapsedColumns } from '../../hooks/useCollapsedColumns';
import { useCollapsedCards } from '../../hooks/useCollapsedCards';
import { Column } from './Column';
import { CardItem } from './CardItem';
import { BoardBand } from './BoardBand';
import { MetricsRibbon } from './MetricsRibbon';
import { SpotlightStrip } from './SpotlightStrip';
import { FilterChipBar } from './FilterChipBar';
import { NowRail, type ActivityEntry } from './NowRail';
import { BoardFooter } from './BoardFooter';
import { BoardSkeleton } from './BoardSkeleton';

const PRIORITY_RANK: Record<string, number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
};

const TYPE_RANK: Record<string, number> = {
  bug: 0,
  feature: 1,
  task: 2,
  subtask: 3,
};

function compareTodoCards(a: Card, b: Card): number {
  const pa = PRIORITY_RANK[a.priority] ?? 999;
  const pb = PRIORITY_RANK[b.priority] ?? 999;
  if (pa !== pb) return pa - pb;
  const ta = TYPE_RANK[a.type] ?? 999;
  const tb = TYPE_RANK[b.type] ?? 999;
  if (ta !== tb) return ta - tb;
  const ca = new Date(a.created).getTime();
  const cb = new Date(b.created).getTime();
  return ca - cb;
}

interface BoardProps {
  cards: Card[];
  config: ProjectConfig;
  loading: boolean;
  error: string | null;
  activeAgents: ActiveAgent[];
  cardsCompletedToday: number;
  cardsCompletedLast7d?: number;
  cardsCompletedPrior7d?: number;
  metricSeries?: MetricSeries;
  runnerMaxAgents?: number;
  runningContainers?: number;
  lastSyncLabel: string;
  activityEntries: ActivityEntry[];
  activityBackfillLoaded?: boolean;
  currentAgent: string | null;
  onCardClick?: (card: Card) => void;
  onCardMove?: (cardId: string, newState: string) => Promise<void>;
  onCreateCard?: (state: string) => void;
  flashCardId?: string | null;
  onParentClick?: (cardId: string) => void;
}

export function Board({
  cards,
  config,
  loading,
  error,
  activeAgents,
  cardsCompletedToday,
  cardsCompletedLast7d,
  cardsCompletedPrior7d,
  metricSeries,
  runnerMaxAgents,
  runningContainers,
  lastSyncLabel,
  activityEntries,
  activityBackfillLoaded,
  currentAgent,
  onCardClick,
  onCardMove,
  onCreateCard,
  flashCardId,
  onParentClick,
}: BoardProps) {
  const [activeCard, setActiveCard] = useState<Card | null>(null);
  const [filter, setFilter] = useState<CardFilter>({});
  const [searchQuery, setSearchQuery] = useState('');
  const isMobile = useMediaQuery('(max-width: 768px)');
  const [nowRailOpen, setNowRailOpen] = useState(false);
  const cardIds = useMemo(() => cards.map((c) => c.id), [cards]);
  const [collapsedColumns, toggleCollapse] = useCollapsedColumns(config.name, config.states);
  const { collapsed: collapsedCards, toggle: toggleCardCollapse, collapseMany, expandMany } = useCollapsedCards(config.name, cardIds);

  // Both sensor hooks are called unconditionally (React rules of hooks).
  // isTouchDevice() selects which pointer-style sensor to pass to useSensors:
  // - Touch: 250ms delay distinguishes press-and-hold drag from scroll.
  // - Pointer: 5px distance threshold for immediate mouse drag.
  // KeyboardSensor is always registered so users can Tab to a card, press
  // Space to pick up, arrow keys to move, Space to drop, Esc to cancel.
  const pointerSensor = useSensor(PointerSensor, { activationConstraint: { distance: 5 } });
  const touchSensor = useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } });
  const keyboardSensor = useSensor(KeyboardSensor, {
    coordinateGetter: sortableKeyboardCoordinates,
  });
  const touchDevice = isTouchDevice();
  const sensors = useSensors(touchDevice ? touchSensor : pointerSensor, keyboardSensor);

  const hasFilter = Object.values(filter).some(Boolean);
  const searchTerm = searchQuery.trim().toLowerCase();
  const hasSearch = searchTerm.length > 0;

  const filteredCards = useMemo(() => {
    if (!hasFilter && !hasSearch) return cards;
    return cards.filter((card) => {
      if (filter.type && card.type !== filter.type) return false;
      if (filter.priority && card.priority !== filter.priority) return false;
      if (filter.label && !(card.labels ?? []).includes(filter.label)) return false;
      if (filter.agent && card.assigned_agent !== filter.agent) return false;
      if (filter.autonomous && !card.autonomous) return false;
      if (filter.runner_status && card.runner_status !== filter.runner_status) return false;
      if (hasSearch) {
        const haystack = [
          card.id,
          card.title,
          card.assigned_agent ?? '',
          card.branch_name ?? '',
          ...(card.labels ?? []),
        ].join(' ').toLowerCase();
        if (!haystack.includes(searchTerm)) return false;
      }
      return true;
    });
  }, [cards, filter, hasFilter, hasSearch, searchTerm]);

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
    for (const state of config.states) {
      if (state === 'todo') {
        grouped[state].sort(compareTodoCards);
      } else {
        grouped[state].sort(
          (a, b) => new Date(b.updated).getTime() - new Date(a.updated).getTime()
        );
      }
    }
    return grouped;
  }, [filteredCards, config.states]);

  useKeyboardShortcuts(
    useMemo(
      () => [
        {
          key: 'Escape',
          handler: () => {
            if (hasFilter) setFilter({});
            if (hasSearch) setSearchQuery('');
          },
        },
      ],
      [hasFilter, hasSearch]
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

  const inFlight = (cardsByState['in_progress']?.length ?? 0) + (cardsByState['review']?.length ?? 0);
  const stalledCount = cardsByState['stalled']?.length ?? cards.filter((c) => c.state === 'stalled').length;
  const openCount = cards.length - (cardsByState['done']?.length ?? 0) - (cardsByState['not_planned']?.length ?? 0);

  return (
    <div className="flex flex-col h-full overflow-y-auto md:overflow-hidden">
      <BoardBand
        projectName={config.name}
        displayName={config.display_name}
        activeAgents={activeAgents.length}
        openCount={openCount}
        inReviewCount={cardsByState['review']?.length ?? 0}
        shippedToday={cardsCompletedToday}
        shippedLast7d={cardsCompletedLast7d}
        shippedPrior7d={cardsCompletedPrior7d}
        lastUpdated={lastSyncLabel}
        onCreateCard={() => onCreateCard?.(config.states[0])}
      />

      <MetricsRibbon
        activeAgents={activeAgents.length}
        inFlight={inFlight}
        stalled={stalledCount}
        shippedToday={cardsCompletedToday}
        shipped7d={cardsCompletedLast7d}
        shipped7dPrior={cardsCompletedPrior7d}
        activeAgentsSeries={metricSeries?.active_agents}
        inFlightSeries={metricSeries?.in_flight}
        stalledSeries={metricSeries?.stalled}
        shippedSeries={metricSeries?.shipped}
      />

      <SpotlightStrip
        cards={cards}
        onCardClick={(cardId) => {
          const c = cards.find((x) => x.id === cardId);
          if (c) onCardClick?.(c);
        }}
      />

      <FilterChipBar
        filter={filter}
        currentAgent={currentAgent}
        onFilterChange={setFilter}
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
      />

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        onDragCancel={handleDragCancel}
      >
        <div className="flex md:flex-1 md:min-h-0 min-h-[calc(100dvh-3rem)]">
          <div className="flex-1 overflow-x-auto overflow-y-hidden">
            <div className="flex gap-3 p-3 sm:gap-4 sm:p-4 h-full min-w-max">
              {config.states.filter((s) => s !== 'stalled').map((state) => (
                <Column
                  key={state}
                  state={state}
                  cards={cardsByState[state]}
                  config={config}
                  collapsed={collapsedColumns.has(state)}
                  onToggleCollapse={toggleCollapse}
                  onCardClick={onCardClick}
                  activeCardState={activeCard?.state}
                  flashCardId={flashCardId}
                  collapsedCards={collapsedCards}
                  onToggleCardCollapse={toggleCardCollapse}
                  onCollapseAll={collapseMany}
                  onExpandAll={expandMany}
                  onParentClick={onParentClick}
                />
              ))}
            </div>
          </div>
          {isMobile && nowRailOpen && (
            <div
              className="now-rail-backdrop"
              onClick={() => setNowRailOpen(false)}
              aria-hidden="true"
            />
          )}
          {nowRailOpen && (
            <NowRail
              agents={activeAgents}
              activityEntries={activityEntries}
              maxAgents={runnerMaxAgents}
              runningContainers={runningContainers}
              hasBackfill={activityBackfillLoaded}
              className={isMobile ? 'animate-panel-slide-in' : undefined}
            />
          )}
        </div>

        <DragOverlay>
          {activeCard ? (
            <div className="w-[260px]"><CardItem card={activeCard} /></div>
          ) : null}
        </DragOverlay>
      </DndContext>

      <BoardFooter
        lastSyncLabel={lastSyncLabel}
        cardCount={cards.length}
        columnCount={config.states.filter((s) => s !== 'stalled').length}
        nowRailOpen={nowRailOpen}
        onToggleNowRail={() => setNowRailOpen((v) => !v)}
      />
    </div>
  );
}
