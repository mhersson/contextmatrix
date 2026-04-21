import { useCallback, useRef, useState } from 'react';
import type { Card, LogEntry, ProjectConfig, PatchCardInput } from '../../types';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelBody } from './CardPanelBody';
import { CardPanelSections } from './CardPanelSections';
import { buildCardPatch, isCardDirty } from './utils';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useBranches } from '../../hooks/useBranches';
import { useCardPanelKeyboard } from '../../hooks/useCardPanelKeyboard';

interface CardPanelProps {
  card: Card;
  config: ProjectConfig;
  cardLogs?: readonly LogEntry[];
  onClose: () => void;
  onSave: (updates: PatchCardInput) => Promise<void>;
  onClaim: (agentId: string) => Promise<void>;
  onRelease: (agentId: string) => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  currentAgentId: string | null;
  onPromptAgentId: () => string | null;
  onRunCard: (interactive: boolean) => Promise<void>;
  onStopCard: () => Promise<void>;
}

export function CardPanel(props: CardPanelProps) {
  const { card, config, cardLogs = [], onClose, onSave, onClaim, onRelease,
    onSubtaskClick, currentAgentId, onPromptAgentId, onRunCard, onStopCard } = props;

  const panelRef = useRef<HTMLDivElement>(null);
  const [editedCard, setEditedCard] = useState(card);
  const [isSaving, setIsSaving] = useState(false);

  const initialIsHITLRunning = card.runner_status === 'running' && !(card.autonomous ?? false);
  const [descriptionCollapsed, setDescriptionCollapsed] = useState(initialIsHITLRunning);
  const [automationCollapsed, setAutomationCollapsed] = useState(initialIsHITLRunning);
  const [labelsCollapsed, setLabelsCollapsed] = useState(initialIsHITLRunning);

  useFocusTrap(panelRef, true);

  // Sync prop → local edited state on card identity change (render-time pattern).
  const [prevCard, setPrevCard] = useState(card);
  if (card !== prevCard) {
    setPrevCard(card);
    setEditedCard(card);
  }

  // Auto-collapse Description, Automation, Labels on entering HITL running;
  // expand on leaving (including promotion mid-run). Fires only on boolean
  // transitions so manual re-expands survive re-renders while still running.
  const isHITLRunning = card.runner_status === 'running' && !(card.autonomous ?? false);
  const [prevIsHITLRunning, setPrevIsHITLRunning] = useState(initialIsHITLRunning);
  if (isHITLRunning !== prevIsHITLRunning) {
    setPrevIsHITLRunning(isHITLRunning);
    setDescriptionCollapsed(isHITLRunning);
    setAutomationCollapsed(isHITLRunning);
    setLabelsCollapsed(isHITLRunning);
  }

  const { branches, loading: branchesLoading, error: branchesError } =
    useBranches(card.project, !!config.remote_execution?.enabled);

  const isDirty = isCardDirty(editedCard, card);
  useCardPanelKeyboard(isDirty, onClose);

  const handleSave = useCallback(async () => {
    if (!isDirty || isSaving) return;
    setIsSaving(true);
    try {
      await onSave(buildCardPatch(editedCard, card));
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, editedCard, card, onSave]);

  const handleClaim = useCallback(async () => {
    const agentId = currentAgentId || onPromptAgentId();
    if (agentId) await onClaim(agentId);
  }, [currentAgentId, onPromptAgentId, onClaim]);

  const handleRelease = useCallback(async () => {
    if (!currentAgentId || !card.assigned_agent) return;
    await onRelease(card.assigned_agent);
  }, [currentAgentId, card.assigned_agent, onRelease]);

  const handleClose = () => {
    if (isDirty && !window.confirm('Discard unsaved changes?')) return;
    onClose();
  };

  const canRun =
    config.remote_execution?.enabled !== false &&
    card.state === 'todo' &&
    (!card.runner_status || card.runner_status === 'failed' || card.runner_status === 'killed');

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={handleClose} />

      <div
        ref={panelRef}
        className="card-panel animate-panel-slide-in"
        role="dialog"
        aria-modal="true"
        aria-label="Card details"
      >
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

        <CardPanelBody card={card} cardLogs={cardLogs} isHITLRunning={isHITLRunning}>
          <CardPanelSections
            card={card}
            editedCard={editedCard}
            setEditedCard={setEditedCard}
            currentAgentId={currentAgentId}
            onClaim={handleClaim}
            onRelease={handleRelease}
            onStopCard={onStopCard}
            onSubtaskClick={onSubtaskClick}
            branches={branches}
            branchesLoading={branchesLoading}
            branchesError={branchesError}
            canRun={canRun}
            isDirty={isDirty}
            onSave={handleSave}
            onRunCard={onRunCard}
            descriptionCollapsed={descriptionCollapsed}
            onToggleDescription={() => setDescriptionCollapsed((v) => !v)}
            automationCollapsed={automationCollapsed}
            onToggleAutomation={() => setAutomationCollapsed((v) => !v)}
            labelsCollapsed={labelsCollapsed}
            onToggleLabels={() => setLabelsCollapsed((v) => !v)}
          />
        </CardPanelBody>
      </div>
    </>
  );
}
