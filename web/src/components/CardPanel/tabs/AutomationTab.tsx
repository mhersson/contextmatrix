import type { Dispatch, SetStateAction } from 'react';
import type { Card } from '../../../types';
import { AutomationCheckboxes } from '../AutomationCheckboxes';
import { CardPanelActivity } from '../CardPanelActivity';

interface AutomationTabProps {
  card: Card;
  editedCard: Card;
  setEditedCard: Dispatch<SetStateAction<Card>>;
  branches: string[];
  branchesLoading: boolean;
  branchesError: boolean;
  editingLocked: boolean;
  automationLockedReason: string;
  forcedFeatureBranch: boolean;
  forcedCreatePR: boolean;
  clearForcedFeatureBranch: () => void;
  clearForcedCreatePR: () => void;
}

/**
 * Automation rail tab — the checkbox rail + inline activity log. All the
 * `onXxxChange` setters that previously inlined as fresh arrows in
 * CardPanelSections now live in this component; they still aren't
 * stable across renders, but that's fine because AutomationCheckboxes
 * itself isn't memoised anymore (the memo was a placebo given the
 * prop shape).
 */
export function AutomationTab({
  card,
  editedCard,
  setEditedCard,
  branches,
  branchesLoading,
  branchesError,
  editingLocked,
  automationLockedReason,
  forcedFeatureBranch,
  forcedCreatePR,
  clearForcedFeatureBranch,
  clearForcedCreatePR,
}: AutomationTabProps) {
  return (
    <div className="bf-auto-wrap">
      <div className="bf-auto-top">
        <AutomationCheckboxes
          autonomous={editedCard.autonomous ?? false}
          useOpusOrchestrator={editedCard.use_opus_orchestrator ?? false}
          featureBranch={editedCard.feature_branch ?? false}
          createPR={editedCard.create_pr ?? false}
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
          branchName={card.branch_name}
          prUrl={card.pr_url}
          reviewAttempts={card.review_attempts}
          baseBranch={editedCard.base_branch}
          onBaseBranchChange={(v) =>
            setEditedCard((prev) => ({ ...prev, base_branch: v || undefined }))
          }
          branches={branches}
          branchesLoading={branchesLoading}
          branchesError={branchesError}
          disabled={editingLocked}
          lockedReason={automationLockedReason}
          forcedFeatureBranch={forcedFeatureBranch}
          forcedCreatePR={forcedCreatePR}
          onClearForcedFeatureBranch={clearForcedFeatureBranch}
          onClearForcedCreatePR={clearForcedCreatePR}
        />
      </div>
      <CardPanelActivity activityLog={card.activity_log} />
    </div>
  );
}
