import type { Card, ProjectConfig } from '../../types';
import { typeColors } from './utils';

interface CardPanelHeaderProps {
  card: Card;
  editedCard: Card;
  config: ProjectConfig;
  isDirty: boolean;
  isSaving: boolean;
  onClose: () => void;
  onSave: () => void;
  onTitleChange: (title: string) => void;
  onPriorityChange: (priority: string) => void;
  onStateChange: (state: string) => void;
}

export function CardPanelHeader({
  card,
  editedCard,
  config,
  isDirty,
  isSaving,
  onClose,
  onSave,
  onTitleChange,
  onPriorityChange,
  onStateChange,
}: CardPanelHeaderProps) {
  const validTransitions = config.transitions[card.state] || [];

  const handleClose = () => {
    if (isDirty) {
      if (window.confirm('Discard unsaved changes?')) onClose();
    } else {
      onClose();
    }
  };

  return (
    <>
      {/* Header bar */}
      <div className="flex items-center justify-between p-4 border-b border-[var(--bg3)]">
        <div className="flex items-center gap-3">
          <button
            onClick={handleClose}
            className="text-[var(--grey1)] hover:text-[var(--fg)] transition-colors"
            title="Close (Esc)"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
          <span className="font-mono text-sm text-[var(--grey1)]">{card.id}</span>
        </div>
        <button
          onClick={onSave}
          disabled={!isDirty || isSaving}
          className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
            isDirty
              ? 'bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90'
              : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
          }`}
        >
          {isSaving ? 'Saving...' : 'Save'}
        </button>
      </div>

      {/* Title */}
      <div className="px-4 pt-3">
        <label className="block text-xs text-[var(--grey1)] mb-1">Title</label>
        <input
          type="text"
          value={editedCard.title}
          onChange={(e) => onTitleChange(e.target.value)}
          className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
        />
      </div>

      {/* Type, Priority, State row */}
      <div className="grid grid-cols-3 gap-3 px-4 pb-3">
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Type</label>
          <div
            className="px-3 py-2 rounded text-sm"
            style={{
              backgroundColor: `color-mix(in srgb, ${typeColors[card.type] || 'var(--grey1)'} 20%, transparent)`,
              color: typeColors[card.type] || 'var(--grey1)',
            }}
          >
            {card.type}
          </div>
        </div>

        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Priority</label>
          <select
            value={editedCard.priority}
            onChange={(e) => onPriorityChange(e.target.value)}
            className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          >
            {config.priorities.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">State</label>
          <select
            value={editedCard.state}
            onChange={(e) => onStateChange(e.target.value)}
            className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          >
            <option value={card.state}>{card.state}</option>
            {validTransitions
              .filter((s) => s !== card.state)
              .map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
          </select>
        </div>
      </div>
    </>
  );
}
