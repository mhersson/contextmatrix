import type { Dispatch, SetStateAction } from 'react';
import type { Card } from '../../types';
import { CardPanelAgent } from './CardPanelAgent';
import { CardPanelEditor } from './CardPanelEditor';
import { CardPanelMetadata } from './CardPanelMetadata';
import { CardPanelActivity } from './CardPanelActivity';

interface CardPanelSectionsProps {
  card: Card;
  editedCard: Card;
  setEditedCard: Dispatch<SetStateAction<Card>>;
  currentAgentId: string | null;
  onClaim: () => Promise<void>;
  onRelease: () => Promise<void>;
  onStopCard: () => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  branches: string[];
  branchesLoading: boolean;
  branchesError: boolean;
  canRun: boolean;
  isDirty: boolean;
  onSave: () => Promise<void>;
  onRunCard: (interactive: boolean) => Promise<void>;
  descriptionCollapsed: boolean;
  onToggleDescription: () => void;
  automationCollapsed: boolean;
  onToggleAutomation: () => void;
  labelsCollapsed: boolean;
  onToggleLabels: () => void;
}

/**
 * Renders the stacked inner sections of the CardPanel body: Agent row,
 * description editor, metadata (labels/automation/etc.), and activity log.
 * Pure prop wiring — no stateful behaviour beyond the `setEditedCard` setter
 * the orchestrator threads through.
 */
export function CardPanelSections({
  card,
  editedCard,
  setEditedCard,
  currentAgentId,
  onClaim,
  onRelease,
  onStopCard,
  onSubtaskClick,
  branches,
  branchesLoading,
  branchesError,
  canRun,
  isDirty,
  onSave,
  onRunCard,
  descriptionCollapsed,
  onToggleDescription,
  automationCollapsed,
  onToggleAutomation,
  labelsCollapsed,
  onToggleLabels,
}: CardPanelSectionsProps) {
  return (
    <>
      <CardPanelAgent
        card={card}
        canClaim={!card.assigned_agent}
        canRelease={!!card.assigned_agent && card.assigned_agent === currentAgentId}
        onClaim={onClaim}
        onRelease={onRelease}
        canStop={card.runner_status === 'queued' || card.runner_status === 'running'}
        onStop={onStopCard}
      />

      <CardPanelEditor
        body={editedCard.body}
        onChange={(body) => setEditedCard((prev) => ({ ...prev, body }))}
        collapsed={descriptionCollapsed}
        onToggleCollapsed={onToggleDescription}
      />

      <CardPanelMetadata
        card={card}
        editedLabels={editedCard.labels}
        onLabelsChange={(labels) => setEditedCard((prev) => ({ ...prev, labels }))}
        onSubtaskClick={onSubtaskClick}
        editedAutonomous={editedCard.autonomous ?? false}
        editedUseOpusOrchestrator={editedCard.use_opus_orchestrator ?? false}
        editedFeatureBranch={editedCard.feature_branch ?? false}
        editedCreatePR={editedCard.create_pr ?? false}
        onAutonomousChange={(v) =>
          setEditedCard((prev) => ({ ...prev, autonomous: v, ...(v ? {} : { base_branch: undefined }) }))
        }
        onUseOpusOrchestratorChange={(v) =>
          setEditedCard((prev) => ({ ...prev, use_opus_orchestrator: v }))
        }
        onFeatureBranchChange={(v) =>
          setEditedCard((prev) => ({
            ...prev,
            feature_branch: v,
            create_pr: v ? prev.create_pr : false,
          }))
        }
        onCreatePRChange={(v) => setEditedCard((prev) => ({ ...prev, create_pr: v }))}
        editedVetted={editedCard.vetted ?? false}
        onVettedChange={(v) => setEditedCard((prev) => ({ ...prev, vetted: v }))}
        baseBranch={editedCard.base_branch}
        onBaseBranchChange={(v) =>
          setEditedCard((prev) => ({ ...prev, base_branch: v || undefined }))
        }
        branches={branches}
        branchesLoading={branchesLoading}
        branchesError={branchesError}
        canRun={canRun}
        onRun={async () => {
          if (isDirty) await onSave();
          await onRunCard(!(editedCard.autonomous ?? false));
        }}
        automationCollapsed={automationCollapsed}
        onToggleAutomation={onToggleAutomation}
        labelsCollapsed={labelsCollapsed}
        onToggleLabels={onToggleLabels}
      />

      <CardPanelActivity activityLog={card.activity_log} />
    </>
  );
}
