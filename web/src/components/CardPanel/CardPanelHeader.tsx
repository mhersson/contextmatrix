import type { Card, ProjectConfig } from '../../types';
import { gitHubIcon, jiraIcon } from '../icons';
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
          {card.source?.system === 'github' && card.source.external_url && (
            <a
              href={card.source.external_url}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-[var(--fg)] transition-colors"
              title={`Open GitHub issue: ${card.source.external_url}`}
            >
              {gitHubIcon}
            </a>
          )}
          {card.source?.system === 'jira' && card.source.external_url && (
            <a
              href={card.source.external_url}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-[var(--fg)] transition-colors"
              title={`Open Jira issue: ${card.source.external_id}`}
            >
              {jiraIcon}
            </a>
          )}
          {card.source && !card.vetted && (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded bg-[var(--bg-yellow)] text-[var(--yellow)] text-xs">
              <svg className="w-4 h-4 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <span>Unvetted external content — agents cannot claim this card</span>
            </div>
          )}
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
