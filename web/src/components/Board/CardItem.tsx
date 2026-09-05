import { memo, useEffect, useRef, useCallback, useState } from 'react';
import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import type { Card } from '../../types';
import { chipTint, priorityColors, typeColors } from '../../lib/chip';
import { cardSignals, HEADER_SIGNAL_CAP } from '../../lib/cardSignals';
import { gitHubIcon } from '../icons';
import { CardChipRow } from './CardChipRow';
import { CardSignalIcons } from './CardSignalIcons';
import { SubtaskStrip, SubtaskPeekList } from './SubtaskStrip';
import { CardHoverCard } from './CardHoverCard';
import { hasUnmetDeps } from '../../lib/chip';
import { lastInputWasKeyboard } from '../../lib/inputModality';

interface CardItemProps {
  card: Card;
  onClick?: () => void;
  flashCardId?: string | null;
  isCollapsed?: boolean;
  onToggleCollapse?: (cardId: string) => void;
  /** Opens a card by id in the panel; used by the subtask peek rows. */
  onParentClick?: (cardId: string) => void;
  subtasks?: Card[];
  /**
   * True for the DragOverlay's copy of the dragged card. Disables the
   * hook so the overlay doesn't register a second droppable whose rect
   * follows the cursor and poisons collision detection.
   */
  dragOverlay?: boolean;
}

const cardIdStyle: React.CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontWeight: 500,
  fontSize: '11px',
  letterSpacing: '0.04em',
  color: 'var(--grey1)',
};

function CardItemImpl({ card, onClick, flashCardId, isCollapsed, onToggleCollapse, onParentClick, subtasks, dragOverlay }: CardItemProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: card.id,
    data: { card },
    disabled: dragOverlay,
    // Keyboard dragging is deliberately unsupported (mouse/touch only), so
    // the default "sortable" role description would announce an affordance
    // screen reader users can't reach. role="button"/tabIndex stay below -
    // Enter still opens the card.
    attributes: { roleDescription: 'card' },
  });

  const cardRef = useRef<HTMLDivElement>(null);
  // Temporary peek: deliberately ephemeral component state, never persisted.
  // An explicit toggle wins; otherwise a flash targeting one of the subtasks
  // holds the peek open, since the flashed row renders nowhere else.
  const [peekChoice, setPeekChoice] = useState<boolean | null>(null);
  const hasSubtasks = (subtasks?.length ?? 0) > 0;
  const subtaskFlash = !!flashCardId && (subtasks ?? []).some((s) => s.id === flashCardId);
  const isFlashing = card.id === flashCardId || subtaskFlash;
  const peekOpen = peekChoice ?? subtaskFlash;

  // Hovercard: expanded, non-overlay cards only. Opens from the header's
  // right-hand group (type pill + icons - present on every card, so a card
  // with labels but no icons still has a target), never from the card body.
  // Closes on leave, on any press (click or drag pickup, so no drag effect is
  // needed), on scroll, on resize, on blur and on Escape while the card has
  // focus. Focus opens it only when the last input was the keyboard (the
  // :focus-visible rule): neither the focus a click confers nor the focus
  // the panel's trap hands back after a mouse click pops the summary.
  // onMouseDown is spread after {...listeners}: PointerSensor uses
  // onPointerDown, but a MouseSensor's activator would be silently
  // discarded, same trap as onKeyDown below.
  const [hoverOpen, setHoverOpen] = useState(false);
  const openHover = useCallback(() => setHoverOpen(true), []);
  const closeHover = useCallback(() => setHoverOpen(false), []);
  const hoverEnabled = !isCollapsed && !dragOverlay;
  const hoverId = `card-hover-${card.id}`;
  useEffect(() => {
    if (!hoverOpen) return;
    window.addEventListener('scroll', closeHover, true);
    window.addEventListener('resize', closeHover);
    return () => {
      window.removeEventListener('scroll', closeHover, true);
      window.removeEventListener('resize', closeHover);
    };
  }, [hoverOpen, closeHover]);
  const describedBy = [attributes['aria-describedby'], hoverOpen ? hoverId : undefined]
    .filter(Boolean)
    .join(' ');
  const hoverRootProps = hoverEnabled
    ? {
        onMouseDown: closeHover,
        onFocus: (e: React.FocusEvent<HTMLDivElement>) => {
          if (e.target !== e.currentTarget || !lastInputWasKeyboard()) return;
          openHover();
        },
        onBlur: (e: React.FocusEvent<HTMLDivElement>) => {
          if (e.target === e.currentTarget) closeHover();
        },
        'aria-describedby': describedBy || undefined,
      }
    : {};

  const setRefs = useCallback((node: HTMLDivElement | null) => {
    setNodeRef(node);
    cardRef.current = node;
  }, [setNodeRef]);

  useEffect(() => {
    if (isFlashing && cardRef.current) {
      cardRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
  }, [isFlashing]);

  const style = {
    transform: CSS.Translate.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const isAgentActive = card.assigned_agent && card.state !== 'stalled';
  const isStalled = card.state === 'stalled';
  const isWorkerFailed = card.worker_status === 'failed';
  const isParked = card.worker_status === 'parked';
  const isNotPlanned = card.state === 'not_planned';

  // Status red (stalled or failed worker) wins over parked yellow, which
  // wins over the claim styling.
  const borderClass = isStalled || isWorkerFailed
    ? 'border-l-[3px] border-l-[var(--red)]'
    : isParked
      ? 'border-l-[3px] border-l-[var(--yellow)]'
      : isNotPlanned
        ? 'border-l-[3px] border-l-[var(--bg4)]'
        : isAgentActive
          ? 'border-l-[3px] border-l-[var(--aqua)] animate-pulse-border'
          : 'border-l-[3px] border-l-transparent';

  // Attached subtasks never reach CardItem, so a board card with a parent is
  // by construction an orphan (its parent left the board list). Keep the
  // subtask tint; the inline stalled/active gradients below still win.
  const orphanTintClass = card.parent
    ? (hasUnmetDeps(card) ? 'card-orphan-tint card-orphan-tint--dep-blocked' : 'card-orphan-tint')
    : '';

  const strip = hasSubtasks && !isCollapsed ? (
    <SubtaskStrip
      subtasks={subtasks!}
      expanded={peekOpen}
      onToggle={() => setPeekChoice(!peekOpen)}
      tall
    />
  ) : null;

  const peekList = hasSubtasks && peekOpen ? (
    <SubtaskPeekList subtasks={subtasks!} onOpen={(id) => onParentClick?.(id)} />
  ) : null;

  const statusBg: React.CSSProperties | undefined = isStalled || isWorkerFailed ? {
    background: 'linear-gradient(90deg, color-mix(in oklab, var(--bg-red) 75%, transparent) 0%, var(--bg1) 50%)',
  } : isParked ? {
    background: 'linear-gradient(90deg, color-mix(in oklab, var(--bg-yellow) 75%, transparent) 0%, var(--bg1) 50%)',
  } : undefined;

  const activeBg: React.CSSProperties | undefined = isAgentActive ? {
    background: 'linear-gradient(90deg, color-mix(in oklab, var(--bg-aqua) 60%, transparent) 0%, var(--bg1) 40%)',
  } : undefined;

  const priorityDot = (size: number) => (
    <span
      className="rounded-full flex-shrink-0"
      style={{ width: size, height: size, backgroundColor: priorityColors[card.priority] || 'var(--grey1)' }}
      role="img"
      aria-label={`Priority: ${card.priority}`}
    />
  );

  const collapseButton = onToggleCollapse ? (
    <button
      onClick={(e) => { e.stopPropagation(); onToggleCollapse(card.id); }}
      className="w-4 h-4 flex items-center justify-center rounded text-[var(--grey1)] hover:text-[var(--fg)] hover:bg-[var(--bg3)] transition-colors flex-shrink-0"
      title={isCollapsed ? 'Expand card' : 'Collapse card'}
      aria-label={isCollapsed ? 'Expand card' : 'Collapse card'}
      aria-expanded={!isCollapsed}
    >
      <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
          d={isCollapsed ? 'M19 9l-7 7-7-7' : 'M5 15l7-7 7 7'} />
      </svg>
    </button>
  ) : null;

  // Enter opens the card (matches onClick). No keyboard drag sensor is
  // registered, so this handler owns onKeyDown outright - if one is ever
  // re-added, note that {...listeners} is spread ahead of onKeyDown below,
  // so React keeps this later prop and the sensor's activator would be
  // silently discarded.
  // Ignore events bubbling from nested buttons (chevron, badges, strip, rows).
  // An Escape that dismissed the hovercard stops there, so the board's
  // document-level Escape does not also clear the filter or search.
  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return;
    if (e.key === 'Escape' && hoverOpen) {
      e.stopPropagation();
      closeHover();
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      onClick?.();
    }
  };

  if (isCollapsed) {
    return (
      <div
        ref={setRefs}
        style={{ ...style, ...(statusBg ?? activeBg) }}
        {...listeners}
        {...attributes}
        onClick={onClick}
        onKeyDown={handleKeyDown}
        aria-label={`Card ${card.id}: ${card.title}`}
        className={`
          bg-[var(--bg1)] rounded-[10px] px-3 py-1.5 mb-2 cursor-grab active:cursor-grabbing
          transition-all duration-150 hover:bg-[var(--bg2)] hover:-translate-y-px hover:shadow-[0_6px_18px_-8px_rgba(0,0,0,0.35)]
          focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)]
          ${borderClass}
          ${orphanTintClass}
          ${isDragging ? 'shadow-lg z-50' : ''}
          ${isFlashing ? 'animate-card-flash' : ''}
        `}
      >
        {/* Collapsed header: priority dot, ID, type badge, title, and toggle
            button. No strip and no signal icons - status reads from the
            border/wash; everything else waits for the expanded view. */}
        <div className="flex items-center gap-2">
          {priorityDot(6)}
          <span className="flex-shrink-0" style={cardIdStyle}>{card.id}</span>
          <CardChipRow card={card} compact />
          <span className="text-xs text-[var(--fg)] truncate min-w-0 flex-1">{card.title}</span>
          {collapseButton}
        </div>
        {peekList}
      </div>
    );
  }

  return (
    <div
      ref={setRefs}
      style={{ ...style, ...(statusBg ?? activeBg) }}
      {...listeners}
      {...attributes}
      {...hoverRootProps}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      aria-label={`Card ${card.id}: ${card.title}`}
      className={`
        bg-[var(--bg1)] rounded-[10px] p-3 mb-2 cursor-grab active:cursor-grabbing
        transition-all duration-150 hover:bg-[var(--bg2)] hover:-translate-y-px hover:shadow-[0_6px_18px_-8px_rgba(0,0,0,0.35)]
        focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)]
        ${borderClass}
        ${orphanTintClass}
        ${isDragging ? 'shadow-lg z-50' : ''}
        ${isFlashing ? 'animate-card-flash' : ''}
      `}
    >
      {/* Header: priority dot, ID, type badge, signal icons, collapse toggle.
          A crowded cluster (HEADER_SIGNAL_CAP+ signals) collapses the type
          pill to its initial so the icons keep fitting in the header. */}
      <div className="flex items-center justify-between mb-2">
        <span className="flex items-center gap-1.5 min-w-0">
          {priorityDot(7)}
          <span style={cardIdStyle}>{card.id}</span>
        </span>
        <div className="flex items-center gap-1.5">
          <div
            className="flex items-center gap-1.5 cursor-pointer"
            onMouseEnter={hoverEnabled ? openHover : undefined}
            onMouseLeave={closeHover}
          >
            {cardSignals(card).length >= HEADER_SIGNAL_CAP ? (
              <span
                className="chip-pill"
                style={chipTint(typeColors[card.type] || 'var(--grey1)')}
                aria-label={`Type: ${card.type}`}
              >
                {card.type.charAt(0)}
              </span>
            ) : (
              <span className="chip-pill" style={chipTint(typeColors[card.type] || 'var(--grey1)')}>
                {card.type}
              </span>
            )}
            <CardSignalIcons card={card} />
            {card.source?.system === 'github' && gitHubIcon}
            {card.source && !card.vetted && (
              <span className="chip-pill flex-shrink-0" style={chipTint('var(--yellow)')}>
                unvetted
              </span>
            )}
          </div>
          {collapseButton}
        </div>
      </div>

      {/* Title */}
      <h3
        className={`mb-2 line-clamp-2 ${isNotPlanned ? 'text-[var(--grey1)]' : 'text-[var(--fg)]'}`}
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: '13.5px',
          fontWeight: 500,
          lineHeight: 1.32,
          letterSpacing: '-0.005em',
        }}
      >
        {card.title}
      </h3>

      {/* Subtask phase strip: one segment per subtask, click to peek */}
      {strip}

      {/* Footer: assignee circle only. */}
      <CardChipRow card={card} />

      {peekList}

      {hoverEnabled && hoverOpen && (
        <CardHoverCard card={card} anchorRef={cardRef} id={hoverId} />
      )}
    </div>
  );
}

export const CardItem = memo(CardItemImpl);
export default CardItem;
