import type { Dispatch, SetStateAction } from 'react';
import type { Card } from '../../../types';
import { useTheme } from '../../../hooks/useTheme';
import { useModelCatalog } from '../../../hooks/useModelCatalog';
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
 * Automation rail tab — the checkbox rail + inline activity log. The
 * `onXxxChange` setters inline as fresh arrows here; they aren't stable
 * across renders, but that's fine because AutomationCheckboxes isn't
 * memoised — memoising it would be a placebo given the prop shape.
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
  const { taskBackend, favorites: favsByTier, bestOfNMax, bestOfNDefault } = useTheme();
  // Card model pins: CM's served catalog (GET /api/models) — the vendor-
  // screened OpenRouter list or the endpoint list. Agent path only.
  const catalog = useModelCatalog(taskBackend === 'agent');
  const models = catalog.models.map((m) => m.id);

  // Flatten all per-tier All slugs into a single de-duplicated list for the
  // chip row. Only relevant when taskBackend === 'agent'; the prop is ignored
  // by AutomationCheckboxes on the runner path.
  const favorites = favsByTier
    ? [...new Set(Object.values(favsByTier).flat())]
    : undefined;

  return (
    <div className="bf-auto-wrap">
      <div className="bf-auto-top">
        <AutomationCheckboxes
          autonomous={editedCard.autonomous ?? false}
          useOpusOrchestrator={editedCard.use_opus_orchestrator ?? false}
          featureBranch={editedCard.feature_branch ?? false}
          createPR={editedCard.create_pr ?? false}
          taskBackend={taskBackend}
          modelOrchestrator={editedCard.model_orchestrator ?? ''}
          modelCoder={editedCard.model_coder ?? ''}
          modelReviewer={editedCard.model_reviewer ?? ''}
          onModelPinChange={(field, value) =>
            setEditedCard((prev) => ({ ...prev, [field]: value }))
          }
          models={models}
          favorites={favorites}
          bestOfN={editedCard.best_of_n}
          bestOfNMax={bestOfNMax}
          bestOfNDefault={bestOfNDefault}
          onBestOfNChange={(v) =>
            setEditedCard((prev) => ({ ...prev, best_of_n: v }))
          }
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
