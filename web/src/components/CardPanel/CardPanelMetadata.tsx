import { useCallback, useEffect, useState } from 'react';
import type { Card } from '../../types';
import { api } from '../../api/client';
import { AutomationCheckboxes } from './AutomationCheckboxes';

interface CardPanelMetadataProps {
  card: Card;
  editedLabels: string[] | undefined;
  onLabelsChange: (labels: string[]) => void;
  onSubtaskClick: (cardId: string) => void;
  editedAutonomous: boolean;
  editedFeatureBranch: boolean;
  editedCreatePR: boolean;
  onAutonomousChange: (value: boolean) => void;
  onFeatureBranchChange: (value: boolean) => void;
  onCreatePRChange: (value: boolean) => void;
  editedVetted: boolean;
  onVettedChange: (value: boolean) => void;
}

export function CardPanelMetadata({
  card,
  editedLabels,
  onLabelsChange,
  onSubtaskClick,
  editedAutonomous,
  editedFeatureBranch,
  editedCreatePR,
  onAutonomousChange,
  onFeatureBranchChange,
  onCreatePRChange,
  editedVetted,
  onVettedChange,
}: CardPanelMetadataProps) {
  const [labelInput, setLabelInput] = useState('');
  const [depStates, setDepStates] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!card.depends_on?.length) {
      setDepStates({});
      return;
    }
    let cancelled = false;
    const fetchDeps = async () => {
      const states: Record<string, string> = {};
      await Promise.all(
        card.depends_on!.map(async (depId) => {
          try {
            const dep = await api.getCard(card.project, depId);
            states[depId] = dep.state;
          } catch {
            states[depId] = 'unknown';
          }
        }),
      );
      if (!cancelled) {
        setDepStates(states);
      }
    };
    fetchDeps();
    return () => { cancelled = true; };
  }, [card.depends_on, card.project]);

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
            className="flex-1 min-w-0 px-3 py-1.5 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-sm text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          />
          <button
            onClick={addLabel}
            className="px-3 py-1.5 rounded bg-[var(--bg3)] text-[var(--grey2)] hover:bg-[var(--bg4)] transition-colors text-sm"
          >
            Add
          </button>
        </div>
      </div>

      {/* Parent */}
      {card.parent && (
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Parent</label>
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => onSubtaskClick(card.parent!)}
              className="px-2 py-1 rounded bg-[var(--bg-blue)] text-[var(--aqua)] hover:bg-[var(--bg3)] transition-colors text-sm font-mono"
            >
              {card.parent}
            </button>
          </div>
        </div>
      )}

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
            {card.depends_on.map((depId) => {
              const state = depStates[depId];
              const isDone = state === 'done';
              return (
                <button
                  key={depId}
                  onClick={() => onSubtaskClick(depId)}
                  className={`px-2 py-1 rounded hover:bg-[var(--bg3)] transition-colors text-sm font-mono ${
                    isDone
                      ? 'bg-[var(--bg-green)] text-[var(--green)]'
                      : 'bg-[var(--bg-red)] text-[var(--red)]'
                  }`}
                >
                  {depId}{state ? ` (${state})` : ''}
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Automation — only for parent/standalone cards */}
      {!card.parent && (
        <AutomationCheckboxes
          autonomous={editedAutonomous}
          featureBranch={editedFeatureBranch}
          createPR={editedCreatePR}
          onAutonomousChange={onAutonomousChange}
          onFeatureBranchChange={onFeatureBranchChange}
          onCreatePRChange={onCreatePRChange}
          branchName={card.branch_name}
          prUrl={card.pr_url}
          reviewAttempts={card.review_attempts}
        />
      )}

      {/* External Import */}
      {card.source && (
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-2">External Import</label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={editedVetted}
              onChange={(e) => onVettedChange(e.target.checked)}
              className="rounded border-[var(--bg3)] bg-[var(--bg2)] accent-[var(--green)]"
            />
            <span className="text-sm text-[var(--fg)]">Content vetted</span>
          </label>
          {!editedVetted && (
            <p className="mt-1 text-xs text-[var(--yellow)]">
              Agents cannot claim this card until it is marked as vetted.
            </p>
          )}
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
