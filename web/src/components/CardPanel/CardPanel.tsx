import { useState, useEffect, useCallback } from 'react';
import MDEditor from '@uiw/react-md-editor';
import { useTheme } from '../../hooks/useTheme';
import type { Card, ProjectConfig, PatchCardInput } from '../../types';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelMetadata } from './CardPanelMetadata';
import { CardPanelAgent } from './CardPanelAgent';
import { CardPanelActivity } from './CardPanelActivity';

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
  const { theme } = useTheme();
  const [editedCard, setEditedCard] = useState(card);
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    setEditedCard(card);
  }, [card]);

  const isDirty =
    editedCard.title !== card.title ||
    editedCard.state !== card.state ||
    editedCard.priority !== card.priority ||
    editedCard.body !== card.body ||
    JSON.stringify(editedCard.labels) !== JSON.stringify(card.labels);

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

      <div className="card-panel animate-panel-slide-in">
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

        <div className="p-4 space-y-4 overflow-y-auto" style={{ maxHeight: 'calc(100vh - 60px)' }}>
          <CardPanelAgent
            card={card}
            canClaim={!card.assigned_agent}
            canRelease={!!card.assigned_agent && card.assigned_agent === currentAgentId}
            onClaim={handleClaim}
            onRelease={handleRelease}
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
          />

          <CardPanelActivity activityLog={card.activity_log} />
        </div>
      </div>
    </>
  );
}
