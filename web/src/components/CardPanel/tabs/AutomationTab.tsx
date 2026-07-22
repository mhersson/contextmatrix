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
}

/**
 * Automation rail tab - the checkbox rail + inline activity log. The
 * `onXxxChange` setters inline as fresh arrows here; they aren't stable
 * across renders, but that's fine because AutomationCheckboxes isn't
 * memoised - memoising it would be a placebo given the prop shape.
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
}: AutomationTabProps) {
  const {
    taskBackend, favorites: favsByTier, bestOfNMax, bestOfNDefault,
    mobMaxParticipants, mobDefaultParticipants, mobGuestNames, mobExecuteCheckpoints,
  } = useTheme();
  // Card model pins: CM's served catalog (GET /api/models) - the vendor-
  // screened OpenRouter list or the endpoint list. Agent path only.
  const catalog = useModelCatalog(taskBackend === 'agent');
  const models = catalog.models.map((m) => m.id);

  // Flatten all per-tier All slugs into a single de-duplicated list for the
  // chip row. Only relevant when taskBackend === 'agent'; the prop is ignored
  // by AutomationCheckboxes on the worker path.
  const favorites = favsByTier
    ? [...new Set(Object.values(favsByTier).flat())]
    : undefined;

  return (
    <div className="bf-auto-wrap">
      <div className="bf-auto-top">
        <AutomationCheckboxes
          autonomous={editedCard.autonomous ?? false}
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
          mobParticipants={editedCard.mob_participants}
          mobMaxParticipants={mobMaxParticipants}
          mobDefaultParticipants={mobDefaultParticipants}
          mobPhases={editedCard.mob_phases}
          mobGuests={editedCard.mob_guests}
          mobGuestNames={mobGuestNames}
          mobExecuteCheckpoints={mobExecuteCheckpoints}
          onMobParticipantsChange={(v) =>
            setEditedCard((prev) => ({ ...prev, mob_participants: v }))
          }
          onMobPhasesChange={(v) =>
            setEditedCard((prev) => ({ ...prev, mob_phases: v }))
          }
          onMobGuestsChange={(v) =>
            setEditedCard((prev) => ({ ...prev, mob_guests: v }))
          }
          onAutonomousChange={(v) =>
            setEditedCard((prev) => ({ ...prev, autonomous: v }))
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
        />
      </div>
      <CardPanelActivity activityLog={card.activity_log} />
    </div>
  );
}
