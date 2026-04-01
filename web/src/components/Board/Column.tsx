import { useDroppable } from '@dnd-kit/core';
import type { Card, ProjectConfig } from '../../types';
import { CardItem } from './CardItem';

interface ColumnProps {
  state: string;
  cards: Card[];
  config: ProjectConfig;
  collapsed?: boolean;
  onToggleCollapse?: (state: string) => void;
  onCardClick?: (card: Card) => void;
  onCreateCard?: (state: string) => void;
  activeCardState?: string | null;
  flashCardId?: string | null;
  collapsedCards?: Set<string>;
  onToggleCardCollapse?: (cardId: string) => void;
  onCollapseAll?: (cardIds: string[]) => void;
  onExpandAll?: (cardIds: string[]) => void;
  onParentClick?: (cardId: string) => void;
}

function formatStateName(state: string): string {
  return state
    .split('_')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

export function Column({ state, cards, config, collapsed, onToggleCollapse, onCardClick, onCreateCard, activeCardState, flashCardId, collapsedCards, onToggleCardCollapse, onCollapseAll, onExpandAll, onParentClick }: ColumnProps) {
  const { setNodeRef, isOver } = useDroppable({
    id: state,
  });

  // Determine if this column is a valid drop target
  const isValidTarget = activeCardState
    ? config.transitions[activeCardState]?.includes(state) || activeCardState === state
    : false;
  const isInvalidTarget = activeCardState && !isValidTarget && activeCardState !== state;

  // Visual feedback classes
  const dropTargetClass = isOver && isValidTarget
    ? 'ring-2 ring-[var(--green)] bg-[var(--bg-green)]'
    : isOver && isInvalidTarget
      ? 'ring-2 ring-[var(--red)] bg-[var(--bg-red)]'
      : '';
  const dimClass = activeCardState && isInvalidTarget ? 'opacity-50' : '';

  // Bulk collapse/expand logic: show button only for 2+ cards
  const cardIds = cards.map((c) => c.id);
  const allCollapsed = cardIds.length >= 2 && cardIds.every((id) => collapsedCards?.has(id));
  const showBulkToggle = cards.length >= 2 && (onCollapseAll || onExpandAll);

  function handleBulkToggle() {
    if (allCollapsed) {
      onExpandAll?.(cardIds);
    } else {
      onCollapseAll?.(cardIds);
    }
  }

  if (collapsed) {
    return (
      <div
        ref={setNodeRef}
        onClick={() => onToggleCollapse?.(state)}
        className={`
          flex-shrink-0 flex flex-col items-center
          bg-[var(--bg0)] rounded-lg border border-[var(--bg3)]
          cursor-pointer hover:bg-[var(--bg1)] transition-all duration-150
          w-10
          ${dropTargetClass}
          ${dimClass}
        `}
      >
        <div className="flex flex-col items-center gap-2 py-3">
          <span className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg2)] text-[var(--grey1)]">
            {cards.length}
          </span>
          <span
            className="text-xs font-medium text-[var(--grey2)] whitespace-nowrap"
            style={{ writingMode: 'vertical-lr' }}
          >
            {formatStateName(state)}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={setNodeRef}
      className={`
        flex-shrink-0 flex flex-col
        bg-[var(--bg0)] rounded-lg border border-[var(--bg3)]
        transition-all duration-150
        ${dropTargetClass}
        ${dimClass}
      `}
      style={{ width: 'var(--col-width)', minWidth: 'var(--col-width)' }}
    >      {/* Column header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-[var(--bg3)]">
        <div className="flex items-center gap-1.5">
          {onToggleCollapse && (
            <button
              onClick={() => onToggleCollapse(state)}
              className="w-5 h-5 flex items-center justify-center rounded text-[var(--grey1)] hover:text-[var(--fg)] hover:bg-[var(--bg2)] transition-colors"
              title="Collapse column"
            >
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
              </svg>
            </button>
          )}
          <h2 className="text-sm font-medium text-[var(--grey2)]">
            {formatStateName(state)}
          </h2>
        </div>
        <div className="flex items-center gap-2">
          {showBulkToggle && (
            <button
              onClick={handleBulkToggle}
              className="w-5 h-5 flex items-center justify-center rounded text-[var(--grey1)] hover:text-[var(--fg)] hover:bg-[var(--bg2)] transition-colors"
              title={allCollapsed ? 'Expand all cards' : 'Collapse all cards'}
            >
              {allCollapsed ? (
                /* Double chevron down — expand all */
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 4l-7 7-7-7" />
                </svg>
              ) : (
                /* Double chevron up — collapse all */
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 20l7-7 7 7" />
                </svg>
              )}
            </button>
          )}
          {onCreateCard && (
            <button
              onClick={() => onCreateCard(state)}
              className="w-5 h-5 flex items-center justify-center rounded text-[var(--grey1)] hover:text-[var(--green)] hover:bg-[var(--bg2)] transition-colors"
              title="New card"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
              </svg>
            </button>
          )}
          <span className="text-xs px-1.5 py-0.5 rounded bg-[var(--bg2)] text-[var(--grey1)]">
            {cards.length}
          </span>
        </div>
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
              flashCardId={flashCardId}
              isCollapsed={collapsedCards?.has(card.id)}
              onToggleCollapse={onToggleCardCollapse}
              onParentClick={onParentClick}
            />
          ))
        )}
      </div>
    </div>
  );
}
