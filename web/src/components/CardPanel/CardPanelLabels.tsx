import { useCallback, useId, useState } from 'react';

interface LabelsSectionProps {
  editedLabels: string[] | undefined;
  disabled: boolean;
  onLabelsChange: (labels: string[]) => void;
  /**
   * Optional message shown next to the locked-state hint. Same pattern as
   * AutomationCheckboxes — caller supplies the reason ("during remote
   * run" vs. "outside todo") so the user knows what to do.
   */
  lockedReason?: string;
}

/**
 * Left-column labels section. Disabled rendering (`disabled=true`) hides
 * add/remove controls and shows a hint when the runner is attached or the
 * card is in a non-todo state.
 */
export function LabelsSection({ editedLabels, disabled, onLabelsChange, lockedReason }: LabelsSectionProps) {
  const [labelInput, setLabelInput] = useState('');
  const [adding, setAdding] = useState(false);
  const inputId = useId();

  const addLabel = useCallback(() => {
    const trimmed = labelInput.trim();
    if (!trimmed) return;
    if (editedLabels?.includes(trimmed)) {
      setLabelInput('');
      setAdding(false);
      return;
    }
    onLabelsChange([...(editedLabels || []), trimmed]);
    setLabelInput('');
    setAdding(false);
  }, [labelInput, editedLabels, onLabelsChange]);

  const removeLabel = useCallback(
    (label: string) => {
      onLabelsChange((editedLabels || []).filter((l) => l !== label));
    },
    [editedLabels, onLabelsChange],
  );

  return (
    <section>
      <div className="section-eyebrow mb-2">Labels</div>
      <div className="flex flex-wrap items-center gap-2">
        {(editedLabels ?? []).map((label) => (
          <span
            key={label}
            className="chip-pill"
            style={{ background: 'var(--bg-purple)', color: 'var(--purple)' }}
          >
            {label}
            {!disabled && (
              <button
                onClick={() => removeLabel(label)}
                className="hover:text-[var(--red)] transition-colors leading-none"
                aria-label={`Remove label ${label}`}
                style={{ fontSize: '13px', opacity: 0.7 }}
              >
                ×
              </button>
            )}
          </span>
        ))}
        {!disabled && !adding && (
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="text-xs px-2 py-1 rounded border border-dashed border-[var(--bg3)] text-[var(--grey1)] hover:text-[var(--fg)] hover:border-[var(--bg4)] transition-colors"
          >
            + add
          </button>
        )}
        {!disabled && adding && (
          <div className="inline-flex items-center gap-1">
            <label htmlFor={inputId} className="sr-only">Add label</label>
            <input
              id={inputId}
              type="text"
              autoFocus
              value={labelInput}
              onChange={(e) => setLabelInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') { e.preventDefault(); addLabel(); }
                if (e.key === 'Escape') { setLabelInput(''); setAdding(false); }
              }}
              placeholder="Add label..."
              className="px-2 py-1 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-xs text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)] w-32"
            />
            <button
              type="button"
              onClick={addLabel}
              className="px-2 py-1 rounded bg-[var(--bg3)] text-xs text-[var(--fg)] hover:bg-[var(--bg4)] transition-colors"
            >
              Add
            </button>
          </div>
        )}
        {disabled && (editedLabels?.length ?? 0) === 0 && (
          <span className="text-xs text-[var(--grey0)] italic">
            No labels · 🔒 {lockedReason ?? 'locked while agent owns this card'}
          </span>
        )}
        {disabled && (editedLabels?.length ?? 0) > 0 && (
          <span className="font-mono ml-1" style={{ fontSize: '11px', color: 'var(--yellow)' }}>
            🔒 {lockedReason ?? 'locked during remote run'}
          </span>
        )}
      </div>
    </section>
  );
}
