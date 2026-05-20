import type { Card, ProjectConfig } from '../../types';
import { gitHubIcon } from '../icons';
import { priorityColors, stateColors, typeColors } from '../../lib/chip';
import { isSafeHttpUrl } from './utils';
import { ChipPicker } from './ChipPicker';

interface CardPanelHeaderChipsProps {
  card: Card;
  editedCard: Card;
  config: ProjectConfig;
  runnerAttached: boolean;
  onClose: () => void;
  onPriorityChange: (priority: string) => void;
  priorityId: string;
  onTypeChange: (type: string) => void;
  typeId: string;
}

/**
 * The left-edge chip cluster for the card-panel header: close button,
 * card ID, type chip, state chip, priority picker, and the imported-from
 * source link. Extracted from CardPanelHeader to keep that file focused
 * on actions and titles. This component is display-only apart from the
 * Priority `<select>`, which is gated by `priorityLocked` (runner attached
 * or card outside todo).
 */
export function CardPanelHeaderChips({
  card,
  editedCard,
  config,
  runnerAttached,
  onClose,
  onPriorityChange,
  priorityId,
  onTypeChange,
  typeId,
}: CardPanelHeaderChipsProps) {
  const typeTint = typeColors[editedCard.type] || 'var(--grey1)';
  const stateTint = stateColors[card.state] || 'var(--grey1)';
  const priorityTint = priorityColors[editedCard.priority] || 'var(--grey1)';

  // Priority — like Automation/Labels — should only be editable while the
  // card is still in `todo`. Outside todo the value already shaped how the
  // last run was queued and editing it would silently drift from history.
  const priorityLocked = runnerAttached || card.state !== 'todo';

  // Type follows the same gating as Priority, plus subtask cards are locked:
  // their type is parent-derived and the server invariant rejects changes.
  const typeLocked =
    runnerAttached || card.state !== 'todo' || card.type === 'subtask';
  const typeLockedTitle =
    card.type === 'subtask'
      ? 'Subtasks cannot change type'
      : runnerAttached
        ? undefined
        : `Type can only be edited in todo · current state: ${card.state.replace(/_/g, ' ')}`;

  // Subtask type chip uses a solid --bg-blue background (matches the parent ID
  // badge family); non-subtask types use the standard tinted look via chipTint.
  const typeChipSolidBg = card.type === 'subtask' ? 'var(--bg-blue)' : undefined;
  const typeOptions =
    card.type === 'subtask'
      ? ['subtask']
      : config.types.filter((t) => t !== 'subtask');

  return (
    <div className="flex items-center gap-2 flex-wrap">
      <button
        onClick={onClose}
        className="text-[var(--grey1)] hover:text-[var(--fg)] transition-colors shrink-0"
        title="Close (Esc)"
        aria-label="Close panel"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
      <span
        style={{
          fontFamily: 'var(--font-mono)',
          fontWeight: 500,
          fontSize: '12px',
          letterSpacing: '0.04em',
          color: 'var(--grey1)',
        }}
      >
        {card.id}
      </span>

      {/* Type picker-chip. Subtask uses the same solid --bg-blue / --aqua
          palette as the parent ID badge so both chips read as the same color
          family on the board and in the panel header; other types use the
          tinted look. The select is disabled on subtasks (parent-derived) and
          outside todo (would silently drift from how the last run was queued). */}
      <label htmlFor={typeId} className="sr-only">Type</label>
      <ChipPicker
        id={typeId}
        value={editedCard.type}
        options={typeOptions}
        tint={typeTint}
        ariaLabel="Type"
        onChange={onTypeChange}
        disabled={typeLocked}
        title={typeLockedTitle}
        solidBg={typeChipSolidBg}
      />

      {/* State chip (display only; transitions go through Info tab). */}
      <span
        className="chip-pill"
        style={{ backgroundColor: `color-mix(in srgb, ${stateTint} 22%, transparent)`, color: stateTint }}
      >
        {card.state.replace(/_/g, ' ')}
      </span>

      {/* Priority picker-chip. */}
      <label htmlFor={priorityId} className="sr-only">Priority</label>
      <ChipPicker
        id={priorityId}
        value={editedCard.priority}
        options={config.priorities}
        tint={priorityTint}
        ariaLabel="Priority"
        onChange={onPriorityChange}
        disabled={priorityLocked}
        title={priorityLocked && !runnerAttached
          ? `Priority can only be edited in todo · current state: ${card.state.replace(/_/g, ' ')}`
          : undefined}
      />

      {card.source && card.source.external_url && isSafeHttpUrl(card.source.external_url) && (
        <a
          href={card.source.external_url}
          target="_blank"
          rel="noopener noreferrer"
          className="bf-source-link"
          title={`Imported from ${card.source.system} · ${card.source.external_id} — opens externally`}
        >
          {card.source.system === 'github' ? (
            <>
              {gitHubIcon}
              <span>#{card.source.external_id}</span>
            </>
          ) : (
            <>
              <span className="font-mono">{card.source.system}</span>
              <span>·</span>
              <span>{card.source.external_id}</span>
            </>
          )}
        </a>
      )}
    </div>
  );
}
