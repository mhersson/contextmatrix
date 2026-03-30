import { useState, useEffect, useCallback } from 'react';
import MDEditor from '@uiw/react-md-editor';
import type { Card, ProjectConfig, PatchCardInput } from '../../types';

interface CardPanelProps {
  card: Card;
  config: ProjectConfig;
  onClose: () => void;
  onSave: (updates: PatchCardInput) => Promise<void>;
  onClaim: (agentId: string) => Promise<void>;
  onRelease: (agentId: string) => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  currentAgentId: string | null;
  onPromptAgentId: () => string | null;
}

const typeColors: Record<string, string> = {
  task: 'var(--blue)',
  bug: 'var(--red)',
  feature: 'var(--green)',
};

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  if (diffMin < 1) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  return `${diffDay}d ago`;
}

export function CardPanel({
  card,
  config,
  onClose,
  onSave,
  onClaim,
  onRelease,
  onSubtaskClick,
  currentAgentId,
  onPromptAgentId,
}: CardPanelProps) {
  const [editedCard, setEditedCard] = useState(card);
  const [isSaving, setIsSaving] = useState(false);
  const [labelInput, setLabelInput] = useState('');

  // Sync with external card updates
  useEffect(() => {
    setEditedCard(card);
  }, [card]);

  const isDirty =
    editedCard.title !== card.title ||
    editedCard.state !== card.state ||
    editedCard.priority !== card.priority ||
    editedCard.body !== card.body ||
    JSON.stringify(editedCard.labels) !== JSON.stringify(card.labels);

  // Escape key handler
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        if (isDirty) {
          if (window.confirm('Discard unsaved changes?')) {
            onClose();
          }
        } else {
          onClose();
        }
      }
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isDirty, onClose]);

  const handleSave = useCallback(async () => {
    if (!isDirty || isSaving) return;

    setIsSaving(true);
    try {
      const updates: PatchCardInput = {};
      if (editedCard.title !== card.title) updates.title = editedCard.title;
      if (editedCard.state !== card.state) updates.state = editedCard.state;
      if (editedCard.priority !== card.priority) updates.priority = editedCard.priority;
      if (editedCard.body !== card.body) updates.body = editedCard.body;
      if (JSON.stringify(editedCard.labels) !== JSON.stringify(card.labels)) {
        updates.labels = editedCard.labels;
      }
      await onSave(updates);
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, editedCard, card, onSave]);

  const handleClaim = useCallback(async () => {
    const agentId = currentAgentId || onPromptAgentId();
    if (agentId) {
      await onClaim(agentId);
    }
  }, [currentAgentId, onPromptAgentId, onClaim]);

  const handleRelease = useCallback(async () => {
    if (currentAgentId) {
      await onRelease(currentAgentId);
    }
  }, [currentAgentId, onRelease]);

  const addLabel = useCallback(() => {
    const trimmed = labelInput.trim();
    if (trimmed && !editedCard.labels?.includes(trimmed)) {
      setEditedCard((prev) => ({
        ...prev,
        labels: [...(prev.labels || []), trimmed],
      }));
      setLabelInput('');
    }
  }, [labelInput, editedCard.labels]);

  const removeLabel = useCallback((label: string) => {
    setEditedCard((prev) => ({
      ...prev,
      labels: (prev.labels || []).filter((l) => l !== label),
    }));
  }, []);

  const canRelease = card.assigned_agent && card.assigned_agent === currentAgentId;
  const canClaim = !card.assigned_agent;

  // Valid state transitions from current state
  const validTransitions = config.transitions[card.state] || [];

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/50 z-40"
        onClick={() => {
          if (isDirty) {
            if (window.confirm('Discard unsaved changes?')) onClose();
          } else {
            onClose();
          }
        }}
      />

      {/* Panel */}
      <div className="card-panel animate-panel-slide-in">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-[var(--bg3)]">
          <div className="flex items-center gap-3">
            <button
              onClick={() => {
                if (isDirty) {
                  if (window.confirm('Discard unsaved changes?')) onClose();
                } else {
                  onClose();
                }
              }}
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
            onClick={handleSave}
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

        {/* Content */}
        <div className="p-4 space-y-4 overflow-y-auto" style={{ maxHeight: 'calc(100vh - 60px)' }}>
          {/* Title */}
          <div>
            <label className="block text-xs text-[var(--grey1)] mb-1">Title</label>
            <input
              type="text"
              value={editedCard.title}
              onChange={(e) => setEditedCard((prev) => ({ ...prev, title: e.target.value }))}
              className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
            />
          </div>

          {/* Type, Priority, State row */}
          <div className="grid grid-cols-3 gap-3">
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
                onChange={(e) => setEditedCard((prev) => ({ ...prev, priority: e.target.value }))}
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
                onChange={(e) => setEditedCard((prev) => ({ ...prev, state: e.target.value }))}
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

          {/* Labels */}
          <div>
            <label className="block text-xs text-[var(--grey1)] mb-1">Labels</label>
            <div className="flex flex-wrap gap-2 mb-2">
              {editedCard.labels?.map((label) => (
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

          {/* Agent section */}
          <div className="p-3 rounded bg-[var(--bg0)] border border-[var(--bg3)]">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-xs text-[var(--grey1)] mb-1">Assigned Agent</div>
                {card.assigned_agent ? (
                  <div className="flex items-center gap-2">
                    <span className="text-sm text-[var(--aqua)]">{card.assigned_agent}</span>
                    {card.last_heartbeat && (
                      <span className="text-xs text-[var(--grey0)]">
                        · {formatRelativeTime(card.last_heartbeat)}
                      </span>
                    )}
                  </div>
                ) : (
                  <span className="text-sm text-[var(--grey0)]">Unassigned</span>
                )}
              </div>
              <div>
                {canClaim && (
                  <button
                    onClick={handleClaim}
                    className="px-3 py-1.5 rounded bg-[var(--bg-blue)] text-[var(--aqua)] hover:opacity-90 transition-opacity text-sm"
                  >
                    Claim
                  </button>
                )}
                {canRelease && (
                  <button
                    onClick={handleRelease}
                    className="px-3 py-1.5 rounded bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90 transition-opacity text-sm"
                  >
                    Release
                  </button>
                )}
              </div>
            </div>
          </div>

          {/* Body (Markdown editor) */}
          <div data-color-mode="dark">
            <label className="block text-xs text-[var(--grey1)] mb-1">Description</label>
            <MDEditor
              value={editedCard.body}
              onChange={(val) => setEditedCard((prev) => ({ ...prev, body: val || '' }))}
              preview="edit"
              height={250}
              visibleDragbar={false}
            />
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

          {/* Activity Log */}
          {card.activity_log && card.activity_log.length > 0 && (
            <div>
              <label className="block text-xs text-[var(--grey1)] mb-2">Activity Log</label>
              <div className="space-y-2 max-h-[200px] overflow-y-auto">
                {[...card.activity_log].reverse().map((entry, idx) => (
                  <div
                    key={idx}
                    className="p-2 rounded bg-[var(--bg0)] border border-[var(--bg3)] text-sm"
                  >
                    <div className="flex items-center gap-2 text-xs text-[var(--grey1)] mb-1">
                      <span className="text-[var(--aqua)]">{entry.agent}</span>
                      <span>·</span>
                      <span>{formatRelativeTime(entry.ts)}</span>
                      <span>·</span>
                      <span className="text-[var(--purple)]">{entry.action}</span>
                    </div>
                    <p className="text-[var(--fg)]">{entry.message}</p>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Metadata footer */}
          <div className="pt-2 border-t border-[var(--bg3)] text-xs text-[var(--grey0)]">
            <div>Created: {new Date(card.created).toLocaleString()}</div>
            <div>Updated: {new Date(card.updated).toLocaleString()}</div>
          </div>
        </div>
      </div>
    </>
  );
}
