import type { Dispatch, SetStateAction } from 'react';
import type { Card } from '../../../types';
import { useTheme } from '../../../hooks/useTheme';
import { useOpenRouterModels } from '../../../hooks/useOpenRouterModels';
import { useChatModels } from '../../../utils/chatModels';
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
  const { taskBackend, favorites: favsByTier } = useTheme();
  // Card model pins use the configured endpoint catalog when
  // llm_endpoint.type=openai (source==='endpoint'), else the live OpenRouter
  // catalog — mirroring the chat model picker. Only consumed on the agent path.
  const { models: endpointModels, source } = useChatModels();
  const orModels = useOpenRouterModels(source !== 'endpoint' && taskBackend === 'agent');
  const models = source === 'endpoint' ? endpointModels.map((m) => m.id) : orModels;

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
