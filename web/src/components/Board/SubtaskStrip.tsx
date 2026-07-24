import type { Card } from '../../types';
import { priorityColors, shortCardId, stripSegClass } from '../../lib/chip';
import { chipClassForState } from '../CardPanel/utils';
import { avatarGradient } from '../../utils/colorHash';

/**
 * Dependency-blocked, matching the red "blocked" deps chip in CardChipRow.
 * dependencies_met uses omitempty on the wire, so absent means false.
 */
export function hasUnmetDeps(card: Card): boolean {
  return (card.depends_on?.length ?? 0) > 0 && !card.dependencies_met;
}

interface SubtaskStripProps {
  subtasks: Card[];
  expanded?: boolean;
  onToggle?: () => void;
  interactive?: boolean;
}

/**
 * Segmented phase strip on a parent card: one segment per subtask, colored by
 * that subtask's state. Interactive mode is a button that toggles the peek
 * list; static mode renders plain segments (no toggle affordance).
 */
export function SubtaskStrip({ subtasks, expanded = false, onToggle, interactive = true }: SubtaskStripProps) {
  const segments = subtasks.map((s) => (
    <span
      key={s.id}
      className={`phase-seg ${stripSegClass(s.state)}`}
      title={`${s.id} · ${s.state}`}
      aria-hidden="true"
    />
  ));

  if (!interactive) {
    return <div className="phase-strip phase-strip--static">{segments}</div>;
  }

  // stopPropagation on keydown as well as click: the card root opens the
  // panel on bubbled Enter, and dnd-kit's root listeners treat bubbled
  // Space as a drag pickup.
  return (
    <button
      type="button"
      className="phase-strip"
      aria-expanded={expanded}
      aria-label={`${subtasks.length} subtask${subtasks.length === 1 ? '' : 's'}`}
      onClick={(e) => { e.stopPropagation(); onToggle?.(); }}
      onKeyDown={(e) => e.stopPropagation()}
    >
      {segments}
    </button>
  );
}

interface SubtaskPeekListProps {
  subtasks: Card[];
  onOpen: (cardId: string) => void;
}

/**
 * One-line subtask rows shown when the phase strip is expanded. Clicking a
 * row opens that subtask in the card panel; rows are never draggable.
 */
export function SubtaskPeekList({ subtasks, onOpen }: SubtaskPeekListProps) {
  return (
    <div className="mt-2">
      {subtasks.map((s) => {
        const depBlocked = hasUnmetDeps(s);
        const agentActive = !!s.assigned_agent && s.state !== 'stalled';
        const rowClass = [
          'peek-row',
          depBlocked ? 'peek-row--dep-blocked' : '',
          agentActive ? 'peek-row--agent' : '',
        ].filter(Boolean).join(' ');
        return (
          <button
            key={s.id}
            type="button"
            className={rowClass}
            title={depBlocked ? `${s.id}: blocked by dependencies` : s.title}
            onClick={(e) => { e.stopPropagation(); onOpen(s.id); }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <span
              className={`chip-pill ${chipClassForState(s.state)}`}
              style={{ fontSize: '10px', padding: '2px 6px' }}
            >
              {s.state.replace(/_/g, ' ')}
            </span>
            <span className="peek-row__id">{shortCardId(s.id)}</span>
            <span className="peek-row__title">{s.title}</span>
            <span
              className="w-1.5 h-1.5 rounded-full flex-shrink-0"
              style={{ backgroundColor: priorityColors[s.priority] || 'var(--grey1)' }}
              aria-label={`Priority: ${s.priority}`}
            />
            {s.assigned_agent && (() => {
              const grad = avatarGradient(s.assigned_agent);
              return (
                <span
                  className="agent-avatar agent-avatar--online flex-shrink-0"
                  style={{ '--av-from': grad.from, '--av-to': grad.to, width: 12, height: 12 } as React.CSSProperties}
                />
              );
            })()}
          </button>
        );
      })}
    </div>
  );
}
