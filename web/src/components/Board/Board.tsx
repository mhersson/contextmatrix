import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  DndContext,
  DragOverlay,
  closestCorners,
  useSensor,
  useSensors,
  PointerSensor,
  TouchSensor,
  type DragStartEvent,
  type DragEndEvent,
} from '@dnd-kit/core';
import type { ActiveAgent, Card, CardFilter, MetricSeries, ProjectConfig, SortMode, SyncStatus } from '../../types';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { safeReadBool, safeWriteBool } from '../../utils/safeStorage';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { useCollapsedColumns } from '../../hooks/useCollapsedColumns';
import { useCollapsedCards } from '../../hooks/useCollapsedCards';
import { useColumnSort } from '../../hooks/useColumnSort';
import { useManualOrder } from '../../hooks/useManualOrder';
import { applyMove } from '../../lib/manualOrder';
import { Column } from './Column';
import { CardItem } from './CardItem';
import { BoardBand } from './BoardBand';
import { BoardMicroBand } from './BoardMicroBand';
import { MetricsRibbon } from './MetricsRibbon';
import { SpotlightStrip } from './SpotlightStrip';
import { FilterChipBar } from './FilterChipBar';
import { NowRail, type ActivityEntry } from './NowRail';
import { BoardFooter } from './BoardFooter';
import { BoardSkeleton } from './BoardSkeleton';
import { deriveMetricsProps } from './metrics';

const NOW_RAIL_STORAGE_KEY = 'contextmatrix-now-rail-open';

// Built-in card types in sort order. Custom types from `.board.yaml` rank
// after these, in the order the board declares them.
const TYPE_ORDER = ['bug', 'feature', 'task', 'subtask'];

// Rank not covered by the board config - sorts last, ties broken by created.
const UNRANKED = Number.MAX_SAFE_INTEGER;

function buildRank(order: string[]): Record<string, number> {
  const rank: Record<string, number> = {};
  for (const key of order) {
    if (!(key in rank)) rank[key] = Object.keys(rank).length;
  }

  return rank;
}

// Card IDs are zero-padded to three digits, so plain lexicographic order breaks
// once a project passes 999 cards (PREFIX-1000 < PREFIX-200). Compare numerically.
function compareIds(a: string, b: string): number {
  return a.localeCompare(b, undefined, { numeric: true });
}

interface BoardProps {
  cards: Card[];
  config: ProjectConfig;
  loading: boolean;
  error: string | null;
  activeAgents: ActiveAgent[];
  cardsCompletedToday: number;
  cardsCompletedTodayParents?: number;
  cardsCompletedLast7d?: number;
  cardsCompletedLast7dParents?: number;
  cardsCompletedPrior7d?: number;
  cardsCompletedPrior7dParents?: number;
  stateCounts?: Record<string, number>;
  stateCountsParents?: Record<string, number>;
  metricSeries?: MetricSeries;
  maxWorkers?: number;
  runningContainers?: number;
  syncStatus?: SyncStatus | null;
  connected?: boolean;
  activityEntries: ActivityEntry[];
  activityBackfillLoaded?: boolean;
  currentAgent: string | null;
  headerCollapsed?: boolean;
  onToggleHeaderCollapsed?: () => void;
  /** Secondary header actions (console/settings/stop-all) rendered left of New Card in whichever band is showing. */
  headerActions?: ReactNode;
  /** Mobile-only sidebar opener forwarded to the band's menu button. */
  onOpenSidebar?: () => void;
  onCardClick?: (card: Card) => void;
  onCardMove?: (cardId: string, newState: string) => Promise<boolean>;
  onCreateCard?: (state: string) => void;
  flashCardId?: string | null;
  onParentClick?: (cardId: string) => void;
  onSyncClick?: () => void;
}

export function Board({
  cards,
  config,
  loading,
  error,
  activeAgents,
  cardsCompletedToday,
  cardsCompletedTodayParents,
  cardsCompletedLast7d,
  cardsCompletedLast7dParents,
  cardsCompletedPrior7d,
  cardsCompletedPrior7dParents,
  stateCounts,
  stateCountsParents,
  metricSeries,
  maxWorkers,
  runningContainers,
  syncStatus,
  connected,
  activityEntries,
  activityBackfillLoaded,
  currentAgent,
  headerCollapsed,
  onToggleHeaderCollapsed,
  headerActions,
  onOpenSidebar,
  onCardClick,
  onCardMove,
  onCreateCard,
  flashCardId,
  onParentClick,
  onSyncClick,
}: BoardProps) {
  const [activeCard, setActiveCard] = useState<Card | null>(null);
  const [filter, setFilter] = useState<CardFilter>({});
  const [searchQuery, setSearchQuery] = useState('');
  const isMobile = useMediaQuery('(max-width: 768px)');
  const [nowRailOpen, setNowRailOpenState] = useState<boolean>(
    () => safeReadBool(NOW_RAIL_STORAGE_KEY) ?? false,
  );
  const setNowRailOpen = (next: boolean) => {
    setNowRailOpenState(next);
    safeWriteBool(NOW_RAIL_STORAGE_KEY, next);
  };
  const cardIds = useMemo(() => cards.map((c) => c.id), [cards]);
  const [collapsedColumns, toggleCollapse] = useCollapsedColumns(config.name, config.states);
  const { collapsed: collapsedCards, toggle: toggleCardCollapse, collapseMany, expandMany } = useCollapsedCards(config.name, cardIds);
  const [getSort, setSort] = useColumnSort(config.name, config.states);
  const { getOrder, hasOrder, setOrder } = useManualOrder(config.name, cards, config.states);

  // Priority ranks come from the board's own scale, most urgent first:
  // `.board.yaml` lists priorities least-urgent-first (low, medium, high,
  // critical), so the declared order is reversed. Types keep the built-in
  // order, with board-specific types appended.
  const priorityRank = useMemo(() => buildRank([...config.priorities].reverse()), [config.priorities]);
  const typeRank = useMemo(() => buildRank([...TYPE_ORDER, ...config.types]), [config.types]);

  // Both sensor hooks are called unconditionally (React rules of hooks).
  // isTouchDevice() selects which pointer-style sensor to pass to useSensors:
  // - Touch: 250ms delay distinguishes press-and-hold drag from scroll.
  // - Pointer: 5px distance threshold for immediate mouse drag.
  const pointerSensor = useSensor(PointerSensor, { activationConstraint: { distance: 5 } });
  const touchSensor = useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } });
  const touchDevice = isTouchDevice();
  const sensors = useSensors(touchDevice ? touchSensor : pointerSensor);

  const hasFilter = Object.values(filter).some(Boolean);
  // Defer the search query so per-keystroke typing is never blocked by the
  // cardsByState sort below. The deferred value lags the real query by at most
  // one frame; React commits the fast path (typing) first, then re-renders with
  // the deferred value (filtering + sorting) in the background.
  const deferredSearchQuery = useDeferredValue(searchQuery);
  const searchTerm = deferredSearchQuery.trim().toLowerCase();
  const hasSearch = searchTerm.length > 0;

  const cardIdSet = useMemo(() => new Set(cards.map((c) => c.id)), [cards]);

  // Attached subtasks (parent present on the board) collapse into the parent's
  // phase strip instead of rendering as column cards. Derived from child
  // parent back-references so it stays live through SSE card replacements.
  const subtasksByParent = useMemo(() => {
    const map = new Map<string, Card[]>();
    for (const card of cards) {
      if (card.parent && cardIdSet.has(card.parent)) {
        const list = map.get(card.parent);
        if (list) {
          list.push(card);
        } else {
          map.set(card.parent, [card]);
        }
      }
    }
    // Zero-padded PREFIX-NNN ids sort lexicographically in creation order.
    for (const list of map.values()) list.sort((a, b) => compareIds(a.id, b.id));
    return map;
  }, [cards, cardIdSet]);

  // Column-renderable cards: parents, standalones, and orphan subtasks whose
  // parent left the board list (graceful degradation).
  const boardCards = useMemo(
    () => cards.filter((c) => !(c.parent && cardIdSet.has(c.parent))),
    [cards, cardIdSet],
  );

  // Resolves a drag-end `over.id` to the card it landed on, so handleDragEnd
  // can derive the target column from wherever the drop occurred.
  const boardCardById = useMemo(
    () => new Map(boardCards.map((c) => [c.id, c])),
    [boardCards],
  );

  const filteredCards = useMemo(() => {
    if (!hasFilter && !hasSearch) return boardCards;
    const matches = (card: Card) => {
      if (filter.type && card.type !== filter.type) return false;
      if (filter.priority && card.priority !== filter.priority) return false;
      if (filter.label && !(card.labels ?? []).includes(filter.label)) return false;
      if (filter.agent && card.assigned_agent !== filter.agent) return false;
      if (filter.assignee && card.assignee !== filter.assignee) return false;
      if (filter.autonomous && !card.autonomous) return false;
      if (filter.worker_status && card.worker_status !== filter.worker_status) return false;
      if (hasSearch) {
        const haystack = [
          card.id,
          card.title,
          card.assigned_agent ?? '',
          card.assignee ?? '',
          card.branch_name ?? '',
          ...(card.labels ?? []),
        ].join(' ').toLowerCase();
        if (!haystack.includes(searchTerm)) return false;
      }
      return true;
    };
    // Family match: a parent stays visible when any of its subtasks matches,
    // so searching a subtask id or filtering by its agent surfaces the family.
    return boardCards.filter(
      (card) => matches(card) || (subtasksByParent.get(card.id) ?? []).some(matches),
    );
  }, [boardCards, subtasksByParent, filter, hasFilter, hasSearch, searchTerm]);

  // Sorts one column's cards under `mode`. Shared between the render-path
  // grouping below and `columnOrder`, which event handlers use to compute the
  // full column order for `applyMove` - never called during render itself.
  const sortColumn = useCallback(
    (state: string, list: Card[], mode: SortMode): Card[] => {
      // Build a timestamp map scoped to this call so comparators don't parse
      // dates per comparison.
      const ts = new Map<string, { created: number; updated: number }>();
      for (const card of list) {
        ts.set(card.id, {
          created: new Date(card.created).getTime(),
          updated: new Date(card.updated).getTime(),
        });
      }
      const created = (c: Card) => ts.get(c.id)?.created ?? 0;
      const updated = (c: Card) => ts.get(c.id)?.updated ?? 0;

      const sorted = [...list];
      switch (mode) {
        case 'id-asc':
          sorted.sort((a, b) => compareIds(a.id, b.id));
          break;
        case 'id-desc':
          sorted.sort((a, b) => compareIds(b.id, a.id));
          break;
        case 'priority':
          sorted.sort(
            (a, b) =>
              (priorityRank[a.priority] ?? UNRANKED) - (priorityRank[b.priority] ?? UNRANKED) ||
              created(a) - created(b),
          );
          break;
        case 'type':
          sorted.sort(
            (a, b) =>
              (typeRank[a.type] ?? UNRANKED) - (typeRank[b.type] ?? UNRANKED) ||
              created(a) - created(b),
          );
          break;
        case 'manual': {
          const order = getOrder(state);
          const index = new Map(order.map((id, i) => [id, i]));
          sorted.sort((a, b) => {
            const ia = index.get(a.id);
            const ib = index.get(b.id);
            if (ia !== undefined && ib !== undefined) return ia - ib;
            if (ia !== undefined) return -1;
            if (ib !== undefined) return 1;
            return updated(b) - updated(a);
          });
          break;
        }
        default:
          sorted.sort((a, b) => updated(b) - updated(a));
      }
      return sorted;
    },
    [priorityRank, typeRank, getOrder],
  );

  // Computes the full (unfiltered) drop-order for one column - the input
  // `applyMove` needs so a search or filter never flattens hidden neighbours
  // out of the stored order. Called only from event handlers.
  const columnOrder = useCallback(
    (state: string): string[] => {
      const list = boardCards.filter((c) => c.state === state);
      return sortColumn(state, list, getSort(state)).map((c) => c.id);
    },
    [boardCards, getSort, sortColumn],
  );

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
      grouped[state] = sortColumn(state, grouped[state], getSort(state));
    }
    return grouped;
  }, [filteredCards, config.states, getSort, sortColumn]);

  // Keep a ref to the latest filter/search booleans so the shortcut handler
  // never needs to be recreated. The ref is read inside the stable wrapper, so
  // the keydown listener is added exactly once and removed only on unmount.
  const escapeStateRef = useRef({ hasFilter, hasSearch });
  useEffect(() => {
    escapeStateRef.current = { hasFilter, hasSearch };
  }, [hasFilter, hasSearch]);

  useKeyboardShortcuts(
    useMemo(
      () => [
        {
          key: 'Escape',
          handler: () => {
            if (escapeStateRef.current.hasFilter) setFilter({});
            if (escapeStateRef.current.hasSearch) setSearchQuery('');
          },
        },
      ],
      [],
    ),
  );

  function handleDragStart(event: DragStartEvent) {
    const card = event.active.data.current?.card as Card | undefined;
    if (card) setActiveCard(card);
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveCard(null);

    if (!over) return;

    const cardId = active.id as string;
    const card = active.data.current?.card as Card | undefined;
    if (!card) return;

    // A drop over a card resolves to that card's column; a drop over a
    // column id resolves directly. Anything else (e.g. a stale id) bails.
    const overId = over.id as string;
    const overCard = boardCardById.get(overId);
    const targetState = overCard ? overCard.state : config.states.includes(overId) ? overId : undefined;
    if (targetState === undefined) return;

    if (targetState === card.state) {
      // A same-column drop expresses an ordering intent only when it lands
      // on another card - a drop on the column body or header (no resolved
      // card) and a drop back on the dragged card itself are both no-ops,
      // skipping both the write and the mode flip.
      if (!overCard) return;
      if (overId === cardId) return;
      setOrder(card.state, applyMove(getOrder(card.state), columnOrder(card.state), cardId, overId));
      if (getSort(card.state) !== 'manual') setSort(card.state, 'manual');
      return;
    }

    if (!onCardMove) return;

    const validTargets = config.transitions[card.state] || [];
    if (!validTargets.includes(targetState)) return;

    // Crossing columns is a state change, not an ordering intent - never
    // flips the target to manual, but a target already on manual records the
    // dropped position (a drop on the column body lands at the end, which
    // applyMove already does for an over-id it can't resolve). The order is
    // computed eagerly, against pre-move data, so the closure doesn't race
    // the state update the move itself triggers; it is written only when
    // onCardMove resolves true, so a rejected or unsuccessful move leaves the
    // stored order untouched.
    const nextOrder = getSort(targetState) === 'manual'
      ? applyMove(getOrder(targetState), columnOrder(targetState), cardId, overId)
      : null;

    // .catch() is chained before .then() so a throw inside setOrder (the
    // .then() callback) is never caught here and misreported as a failed
    // move.
    onCardMove(cardId, targetState)
      .catch((err: unknown) => {
        console.warn(`move ${cardId} -> ${targetState} failed`, err);
        return false;
      })
      .then((ok) => {
        // The moved card is judged against targetState, not the state it still
        // carries in `cards` at this point - otherwise a drop out of a terminal
        // column is pruned away as terminal and the dropped position is lost.
        if (ok && nextOrder) setOrder(targetState, nextOrder, cardId);
      });
  }

  // The collapse toggle lives inside whichever band is showing, so toggling
  // unmounts the focused button. Remember whether it had focus and move focus
  // to its replacement after the swap, so keyboard users can toggle again and
  // screen readers hear the new aria-expanded state.
  const headerToggleRef = useRef<HTMLButtonElement>(null);
  const refocusHeaderToggle = useRef(false);
  const handleToggleHeader = useCallback(() => {
    refocusHeaderToggle.current = document.activeElement === headerToggleRef.current;
    onToggleHeaderCollapsed?.();
  }, [onToggleHeaderCollapsed]);
  useEffect(() => {
    if (!refocusHeaderToggle.current) return;
    refocusHeaderToggle.current = false;
    headerToggleRef.current?.focus();
  }, [headerCollapsed]);
  const toggleHeaderProps = onToggleHeaderCollapsed
    ? { onToggleCollapsed: handleToggleHeader, toggleRef: headerToggleRef }
    : {};

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

  const {
    openCount,
    inReviewCount,
    shippedTodayParents,
    shippedLast7dParents,
    shippedPrior7dParents,
    inFlightParents,
    inFlightSubtasks,
    stalledParents,
    stalledSubtasks,
    shippedTodaySubtasks,
    shipped7dSubtasks,
  } = deriveMetricsProps({
    stateCounts,
    stateCountsParents,
    cards,
    cardsCompletedToday,
    cardsCompletedTodayParents,
    cardsCompletedLast7d,
    cardsCompletedLast7dParents,
    cardsCompletedPrior7d,
    cardsCompletedPrior7dParents,
  });

  // The spotlight strip is the only place stalled/blocked cards surface (the
  // stalled column is filtered out of the kanban), so the collapsed header
  // drops only its all-clear form - never the strip itself.
  const hasAttention = cards.some((c) => c.state === 'stalled' || c.state === 'blocked');

  return (
    <div className="flex flex-col h-full overflow-y-auto md:overflow-hidden">
      {headerCollapsed ? (
        <BoardMicroBand
          projectName={config.name}
          displayName={config.display_name}
          activeAgents={activeAgents.length}
          openCount={openCount}
          inReviewCount={inReviewCount}
          stalled={stalledParents}
          shippedToday={shippedTodayParents}
          shippedLast7d={shippedLast7dParents}
          shippedPrior7d={shippedPrior7dParents}
          onCreateCard={() => onCreateCard?.(config.states[0])}
          actions={headerActions}
          onOpenSidebar={onOpenSidebar}
          {...toggleHeaderProps}
        />
      ) : (
        <>
          <BoardBand
            projectName={config.name}
            displayName={config.display_name}
            activeAgents={activeAgents.length}
            openCount={openCount}
            inReviewCount={inReviewCount}
            shippedToday={shippedTodayParents}
            shippedLast7d={shippedLast7dParents}
            shippedPrior7d={shippedPrior7dParents}
            onCreateCard={() => onCreateCard?.(config.states[0])}
            actions={headerActions}
            onOpenSidebar={onOpenSidebar}
            {...toggleHeaderProps}
          />

          <MetricsRibbon
            activeAgents={activeAgents.length}
            inFlight={inFlightParents}
            inFlightSubtasks={inFlightSubtasks}
            stalled={stalledParents}
            stalledSubtasks={stalledSubtasks}
            shippedToday={shippedTodayParents}
            shippedTodaySubtasks={shippedTodaySubtasks}
            shipped7d={shippedLast7dParents}
            shipped7dSubtasks={shipped7dSubtasks}
            shipped7dPrior={shippedPrior7dParents}
            activeAgentsSeries={metricSeries?.active_agents}
            inFlightSeries={metricSeries?.in_flight_parents}
            stalledSeries={metricSeries?.stalled_parents}
            shippedSeries={metricSeries?.shipped_parents}
          />
        </>
      )}

      {(!headerCollapsed || hasAttention) && (
        <SpotlightStrip
          cards={cards}
          subtasksByParent={subtasksByParent}
          flashCardId={flashCardId}
          onCardClick={(cardId) => {
            const c = cards.find((x) => x.id === cardId);
            if (c) onCardClick?.(c);
          }}
        />
      )}

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
        accessibility={{
          screenReaderInstructions: {
            draggable:
              'Drag a card with the mouse or by touch to move it to another column, or drop it onto another card to reorder it within a column.',
          },
        }}
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
                  sortMode={getSort(state)}
                  onSortChange={(mode) => {
                    // Seed under the outgoing mode before flipping, so
                    // selecting Manual with nothing stored captures the
                    // order the user was already looking at instead of
                    // reshuffling the column.
                    if (mode === 'manual' && !hasOrder(state)) setOrder(state, columnOrder(state));
                    setSort(state, mode);
                  }}
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
                  subtasksByParent={subtasksByParent}
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
              maxAgents={maxWorkers}
              runningContainers={runningContainers}
              hasBackfill={activityBackfillLoaded}
              className={isMobile ? 'animate-panel-slide-in' : undefined}
            />
          )}
        </div>

        <DragOverlay>
          {activeCard ? (
            <div className="w-[260px]">
              <CardItem card={activeCard} subtasks={subtasksByParent.get(activeCard.id)} dragOverlay />
            </div>
          ) : null}
        </DragOverlay>
      </DndContext>

      <BoardFooter
        syncStatus={syncStatus}
        connected={connected}
        cardCount={cards.length}
        columnCount={config.states.filter((s) => s !== 'stalled').length}
        nowRailOpen={nowRailOpen}
        onToggleNowRail={() => setNowRailOpen(!nowRailOpen)}
        onSyncClick={onSyncClick}
      />
    </div>
  );
}
