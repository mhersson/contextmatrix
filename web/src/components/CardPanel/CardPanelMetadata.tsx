import { useCallback, useState } from 'react';
import type { Card } from '../../types';

interface CardPanelMetadataProps {
  card: Card;
  editedLabels: string[] | undefined;
  onLabelsChange: (labels: string[]) => void;
  onSubtaskClick: (cardId: string) => void;
}

export function CardPanelMetadata({
  card,
  editedLabels,
  onLabelsChange,
  onSubtaskClick,
}: CardPanelMetadataProps) {
  const [labelInput, setLabelInput] = useState('');

  const addLabel = useCallback(() => {
    const trimmed = labelInput.trim();
    if (trimmed && !editedLabels?.includes(trimmed)) {
      onLabelsChange([...(editedLabels || []), trimmed]);
      setLabelInput('');
    }
  }, [labelInput, editedLabels, onLabelsChange]);

  const removeLabel = useCallback(
    (label: string) => {
      onLabelsChange((editedLabels || []).filter((l) => l !== label));
    },
    [editedLabels, onLabelsChange],
  );

  return (
    <>
      {/* Labels */}
      <div>
        <label className="block text-xs text-[var(--grey1)] mb-1">Labels</label>
        <div className="flex flex-wrap gap-2 mb-2">
          {editedLabels?.map((label) => (
            <span
              key={label}
              className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded bg-[var(--bg-purple)] text-[var(--purple)]"
            >
              {label}
              <button
                onClick={() => removeLabel(label)}
                className="hover:text-[var(--red)] transition-colors"
              >
                ×
              </button>
            </span>
          ))}
        </div>
        <div className="flex gap-2">
          <input
            type="text"
            value={labelInput}
            onChange={(e) => setLabelInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && addLabel()}
            placeholder="Add label..."
            className="flex-1 px-3 py-1.5 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-sm text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          />
          <button
            onClick={addLabel}
            className="px-3 py-1.5 rounded bg-[var(--bg3)] text-[var(--grey2)] hover:bg-[var(--bg4)] transition-colors text-sm"
          >
            Add
          </button>
        </div>
      </div>

      {/* Subtasks */}
      {card.subtasks && card.subtasks.length > 0 && (
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Subtasks</label>
          <div className="flex flex-wrap gap-2">
            {card.subtasks.map((subtaskId) => (
              <button
                key={subtaskId}
                onClick={() => onSubtaskClick(subtaskId)}
                className="px-2 py-1 rounded bg-[var(--bg2)] text-[var(--aqua)] hover:bg-[var(--bg3)] transition-colors text-sm font-mono"
              >
                {subtaskId}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Dependencies */}
      {card.depends_on && card.depends_on.length > 0 && (
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Dependencies</label>
          <div className="flex flex-wrap gap-2">
            {card.depends_on.map((depId) => (
              <button
                key={depId}
                onClick={() => onSubtaskClick(depId)}
                className="px-2 py-1 rounded bg-[var(--bg2)] text-[var(--yellow)] hover:bg-[var(--bg3)] transition-colors text-sm font-mono"
              >
                {depId}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Metadata footer */}
      <div className="pt-2 border-t border-[var(--bg3)] text-xs text-[var(--grey0)]">
        <div>Created: {new Date(card.created).toLocaleString()}</div>
        <div>Updated: {new Date(card.updated).toLocaleString()}</div>
      </div>
    </>
  );
}
