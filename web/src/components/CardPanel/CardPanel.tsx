import { useState, useEffect, useCallback, useRef } from 'react';
import MDEditor from '@uiw/react-md-editor';
import { useTheme } from '../../hooks/useTheme';
import type { Card, ProjectConfig, PatchCardInput } from '../../types';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelMetadata } from './CardPanelMetadata';
import { CardPanelAgent } from './CardPanelAgent';
import { CardPanelActivity } from './CardPanelActivity';
import { useFocusTrap } from '../../hooks/useFocusTrap';

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
  onRunCard: () => Promise<void>;
  onStopCard: () => Promise<void>;
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
  onRunCard,
  onStopCard,
}: CardPanelProps) {
  const { theme } = useTheme();
  const panelRef = useRef<HTMLDivElement>(null);
  const [editedCard, setEditedCard] = useState(card);
  const [isSaving, setIsSaving] = useState(false);

  useFocusTrap(panelRef, true);

  useEffect(() => {
    setEditedCard(card);
  }, [card]);

  const isDirty =
    editedCard.title !== card.title ||
    editedCard.state !== card.state ||
    editedCard.priority !== card.priority ||
    editedCard.body !== card.body ||
    JSON.stringify(editedCard.labels) !== JSON.stringify(card.labels) ||
    (editedCard.autonomous ?? false) !== (card.autonomous ?? false) ||
    (editedCard.feature_branch ?? false) !== (card.feature_branch ?? false) ||
    (editedCard.create_pr ?? false) !== (card.create_pr ?? false);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        if (isDirty) {
          if (window.confirm('Discard unsaved changes?')) onClose();
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
      if ((editedCard.autonomous ?? false) !== (card.autonomous ?? false)) {
        updates.autonomous = editedCard.autonomous ?? false;
      }
      if ((editedCard.feature_branch ?? false) !== (card.feature_branch ?? false)) {
        updates.feature_branch = editedCard.feature_branch ?? false;
      }
      if ((editedCard.create_pr ?? false) !== (card.create_pr ?? false)) {
        updates.create_pr = editedCard.create_pr ?? false;
      }
      await onSave(updates);
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, editedCard, card, onSave]);

  const handleClaim = useCallback(async () => {
    const agentId = currentAgentId || onPromptAgentId();
    if (agentId) await onClaim(agentId);
  }, [currentAgentId, onPromptAgentId, onClaim]);

  const handleRelease = useCallback(async () => {
    if (currentAgentId) await onRelease(currentAgentId);
  }, [currentAgentId, onRelease]);

  const handleClose = () => {
    if (isDirty) {
      if (window.confirm('Discard unsaved changes?')) onClose();
    } else {
      onClose();
    }
  };

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={handleClose} />

      <div ref={panelRef} className="card-panel animate-panel-slide-in" role="dialog" aria-modal="true" aria-label="Card details">
        <CardPanelHeader
          card={card}
          editedCard={editedCard}
          config={config}
          isDirty={isDirty}
          isSaving={isSaving}
          onClose={onClose}
          onSave={handleSave}
          onTitleChange={(title) => setEditedCard((prev) => ({ ...prev, title }))}
          onPriorityChange={(priority) => setEditedCard((prev) => ({ ...prev, priority }))}
          onStateChange={(state) => setEditedCard((prev) => ({ ...prev, state }))}
        />

        <div className="p-4 space-y-4 overflow-y-auto overflow-x-hidden" style={{ maxHeight: 'calc(100vh - 60px)' }}>
          <CardPanelAgent
            card={card}
            canClaim={!card.assigned_agent}
            canRelease={!!card.assigned_agent && card.assigned_agent === currentAgentId}
            onClaim={handleClaim}
            onRelease={handleRelease}
            canRun={!!card.autonomous && card.state === 'todo' && (!card.runner_status || card.runner_status === 'failed' || card.runner_status === 'killed') && config.remote_execution?.enabled !== false}
            canStop={card.runner_status === 'queued' || card.runner_status === 'running'}
            onRun={onRunCard}
            onStop={onStopCard}
          />

          <div data-color-mode={theme}>
            <label className="block text-xs text-[var(--grey1)] mb-1">Description</label>
            <MDEditor
              value={editedCard.body}
              onChange={(val) => setEditedCard((prev) => ({ ...prev, body: val || '' }))}
              preview="edit"
              height={250}
              visibleDragbar={false}
            />
          </div>

          <CardPanelMetadata
            card={card}
            editedLabels={editedCard.labels}
            onLabelsChange={(labels) => setEditedCard((prev) => ({ ...prev, labels }))}
            onSubtaskClick={onSubtaskClick}
            editedAutonomous={editedCard.autonomous ?? false}
            editedFeatureBranch={editedCard.feature_branch ?? false}
            editedCreatePR={editedCard.create_pr ?? false}
            onAutonomousChange={(v) => setEditedCard((prev) => ({ ...prev, autonomous: v }))}
            onFeatureBranchChange={(v) => setEditedCard((prev) => ({
              ...prev,
              feature_branch: v,
              create_pr: v ? prev.create_pr : false,
            }))}
            onCreatePRChange={(v) => setEditedCard((prev) => ({ ...prev, create_pr: v }))}
          />

          <CardPanelActivity activityLog={card.activity_log} />
        </div>
      </div>
    </>
  );
}
