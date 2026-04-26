import { useCallback, useRef, useState } from 'react';
import type { Card, LogEntry, ProjectConfig, PatchCardInput } from '../../types';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelBody, type RailTabKey } from './CardPanelBody';
import { CardPanelLeft } from './CardPanelLeft';
import { buildCardPanelTabs } from './buildCardPanelTabs';
import { buildCardPatch, isCardDirty, isRunnerAttached, primaryAction } from './utils';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useBranches } from '../../hooks/useBranches';
import { useCardPanelKeyboard } from '../../hooks/useCardPanelKeyboard';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

interface CardPanelProps {
  card: Card;
  config: ProjectConfig;
  cardLogs?: readonly LogEntry[];
  onClose: () => void;
  onSave: (updates: PatchCardInput) => Promise<void>;
  onDelete: (cardId: string) => Promise<void>;
  onClaim: (agentId: string) => Promise<void>;
  onRelease: (agentId: string) => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  currentAgentId: string | null;
  onPromptAgentId: () => string | null;
  onRunCard: (interactive: boolean) => Promise<void>;
  onStopCard: () => Promise<void>;
}

/**
 * Bifold card detail panel — full-width header row over a two-column body
 * (left: labels + description, right: tabbed rail). The rail can be expanded
 * to widen the whole drawer and reshape the grid to 40/60 so the plan and
 * the rail can sit side-by-side.
 *
 * State layering:
 *   - `editedCard` holds in-flight edits; `isCardDirty` drives the Save
 *     button and the save-before-run ordering.
 *   - `railExpanded` persists across tab switches, card-state changes, and
 *     SSE-driven card refreshes; resets only on card identity change.
 *   - `activeTab` persists similarly; resets on card identity change or when
 *     the default tab changes (e.g. entering/leaving HITL).
 *   - `forcedFeatureBranch` / `forcedCreatePR` capture the pre-Run values of
 *     those two flags so the Automation tab can render the `⚡ forced on
 *     run` badge only when the current Run click actually flipped them.
 *
 * The Run handler (and the header's primary action) always awaits `onSave`
 * before firing the runner webhook, matching the server's force-enable
 * behavior in `internal/api/runner.go:runCard`.
 */
export function CardPanel(props: CardPanelProps) {
  const { card, config, cardLogs = [], onClose, onSave, onDelete, onClaim, onRelease,
    onSubtaskClick, currentAgentId, onPromptAgentId, onRunCard, onStopCard } = props;

  const panelRef = useRef<HTMLDivElement>(null);
  const [editedCard, setEditedCard] = useState(card);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  useFocusTrap(panelRef, true);

  const isMobile = useMediaQuery('(max-width: 768px)');
  const isHITLRunning = card.runner_status === 'running' && !(card.autonomous ?? false);
  const defaultTab: RailTabKey = isHITLRunning ? 'chat' : isMobile ? 'card' : 'automation';

  const [railExpanded, setRailExpanded] = useState(isHITLRunning);
  const [activeTab, setActiveTab] = useState<RailTabKey>(defaultTab);
  const [forcedFeatureBranch, setForcedFeatureBranch] = useState(false);
  const [forcedCreatePR, setForcedCreatePR] = useState(false);
  const [confirmDiscardOpen, setConfirmDiscardOpen] = useState(false);

  // Sync derived state with prop changes. Reset the rail/tab/badges only on
  // card identity change (user selected a different card) — SSE-driven
  // refreshes of the same card (state transitions, log additions, etc.) must
  // not collapse the rail mid-session. The edit buffer is refreshed whenever
  // the card object reference changes so unedited fields reflect server-side
  // updates; activeTab resets only when `isHITLRunning` flips, since that is
  // what changes the default tab set.
  // hitlOffCount: how many consecutive sync events have observed
  // isHITLRunning===false since the first true→false flip. The chat tab only
  // collapses once this reaches 2, so a single-render transient SSE glitch
  // (runner_status briefly stale) does not switch the tab away. Resets on
  // HITL-on flip, on card-id change, and on user-initiated tab change.
  const [sync, setSync] = useState({ cardId: card.id, card, isHITLRunning, hitlOffCount: 0 });
  if (sync.cardId !== card.id) {
    setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: 0 });
    setEditedCard(card);
    setRailExpanded(isHITLRunning);
    setForcedFeatureBranch(false);
    setForcedCreatePR(false);
    setActiveTab(defaultTab);
  } else if (sync.card !== card || sync.isHITLRunning !== isHITLRunning) {
    const hitlFlipped = sync.isHITLRunning !== isHITLRunning;
    const cardRefChanged = sync.card !== card;
    if (cardRefChanged) setEditedCard(card);
    if (hitlFlipped && isHITLRunning) {
      // HITL turned on: reset stability counter and switch to chat immediately.
      setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: 0 });
      setActiveTab(defaultTab);
      setRailExpanded(true);
    } else if (!isHITLRunning && (hitlFlipped || sync.hitlOffCount > 0)) {
      // HITL is off and we're either in the initial flip or already counting:
      // require two consecutive false-state sync events before collapsing the
      // chat tab so a single-render transient SSE glitch is ignored.
      const newCount = sync.hitlOffCount + 1;
      setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: newCount });
      if (newCount >= 2) {
        setActiveTab(defaultTab);
      }
    } else {
      setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: sync.hitlOffCount });
    }
  }

  const { branches, loading: branchesLoading, error: branchesError } =
    useBranches(card.project, !!config.remote_execution?.enabled);

  const isDirty = isCardDirty(editedCard, card);

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

  const canDelete =
    (card.state === 'todo' || card.state === 'not_planned') && !card.assigned_agent;

  const handleDelete = useCallback(async () => {
    if (!canDelete) return;
    setIsDeleting(true);
    try {
      await onDelete(card.id);
    } finally {
      setIsDeleting(false);
    }
  }, [canDelete, card.id, onDelete]);

  const handleClose = useCallback(() => {
    if (isDirty) {
      setConfirmDiscardOpen(true);
      return;
    }
    onClose();
  }, [isDirty, onClose]);

  useCardPanelKeyboard(handleClose, handleSave);

  const canRun =
    config.remote_execution?.enabled !== false &&
    card.state === 'todo' &&
    (!card.runner_status || card.runner_status === 'failed' || card.runner_status === 'killed');

  const runnerAttached = isRunnerAttached(card, currentAgentId);
  const primary = primaryAction(card, editedCard.autonomous ?? false, config, canRun);

  // First unfinished dep for the "Open dependency" helper (blocked cards).
  const firstUnfinishedDep =
    card.state === 'blocked' && card.depends_on && card.depends_on.length > 0
      ? card.depends_on[0]
      : null;

  /**
   * Run handler shared by the header Run button and any wrappers.
   * Server force-enables feature_branch and create_pr on every run. We mirror
   * that client-side so the saved state matches, and capture the pre-force
   * values so the UI can badge the forced flags exactly once.
   *
   * On save failure: revert only the two fields we optimistically mutated
   * (feature_branch, create_pr) via a functional update — leaves any
   * concurrent user edits (title, labels, etc.) intact. The toast is fired
   * by useCardActions; we swallow here to avoid an unhandled rejection.
   */
  const handleRun = useCallback(async () => {
    const wasFeatureBranch = editedCard.feature_branch ?? false;
    const wasCreatePR = editedCard.create_pr ?? false;
    setForcedFeatureBranch(!wasFeatureBranch);
    setForcedCreatePR(!wasCreatePR);
    const next: Card = {
      ...editedCard,
      feature_branch: true,
      create_pr: true,
    };
    setEditedCard(next);
    const nextIsDirty = isCardDirty(next, card);
    if (nextIsDirty) {
      setIsSaving(true);
      try {
        await onSave(buildCardPatch(next, card));
      } catch {
        setEditedCard((curr) => ({
          ...curr,
          feature_branch: wasFeatureBranch,
          create_pr: wasCreatePR,
        }));
        setForcedFeatureBranch(false);
        setForcedCreatePR(false);
        return;
      } finally {
        setIsSaving(false);
      }
    }
    try {
      await onRunCard(!(next.autonomous ?? false));
    } catch {
      // Save succeeded but the runner webhook failed. The feature_branch /
      // create_pr values on the server are now real, so don't revert those;
      // clear the "forced on run" badges since they only make sense next
      // to a live runner claim.
      setForcedFeatureBranch(false);
      setForcedCreatePR(false);
    }
  }, [editedCard, card, onSave, onRunCard]);

  const handleTransitionPrimary = useCallback(async (targetState: string) => {
    const prevState = editedCard.state;
    const next: Card = { ...editedCard, state: targetState };
    setEditedCard(next);
    if (isCardDirty(next, card) && !isSaving) {
      setIsSaving(true);
      try {
        await onSave(buildCardPatch(next, card));
      } catch {
        // Revert the optimistic state change; keep any other concurrent edits.
        setEditedCard((curr) => ({ ...curr, state: prevState }));
      } finally {
        setIsSaving(false);
      }
    }
  }, [editedCard, card, onSave, isSaving]);

  const handlePrimary = useCallback(() => {
    if (!primary) return;
    if (primary.kind === 'run') void handleRun();
    else if (primary.kind === 'transition') void handleTransitionPrimary(primary.targetState);
    // 'stop' is handled inline by the header (has its own confirm flow).
  }, [primary, handleRun, handleTransitionPrimary]);

  const excludeStateFromPicker = primary?.kind === 'transition' ? primary.targetState : null;

  const deleteTooltip = canDelete
    ? `Delete card ${card.id}`
    : 'Only unclaimed cards in todo or not_planned can be deleted';

  // Editable surfaces lock under two predicates with different remediations.
  // Runner-attached is automatic and clears when the runner releases.
  // State-driven asks the user to move the card back to `todo` — the only
  // state that can re-run. Both feed the left column (Labels) and the
  // Automation tab (checkbox rail), so compute once here.
  const isTodo = card.state === 'todo';
  const stateLocksEditing = !isTodo && !runnerAttached;
  const editingLocked = runnerAttached || stateLocksEditing;
  const lockedReason = runnerAttached
    ? 'locked during remote run'
    : `locked outside todo · move card back to todo to edit (current: ${card.state.replace(/_/g, ' ')})`;
  const automationLockedReason = runnerAttached
    ? 'Automation locked during remote run'
    : `Automation can only be edited in todo · current state: ${card.state.replace(/_/g, ' ')}`;
  const canToggleEditor =
    !runnerAttached &&
    (card.state === 'todo' || card.state === 'done' || card.state === 'not_planned');

  const { tabs, defaultTab: resolvedDefaultTab } = buildCardPanelTabs({
    card,
    editedCard,
    setEditedCard,
    config,
    cardLogs,
    currentAgentId,
    runnerAttached,
    isHITLRunning,
    onClaim: handleClaim,
    onRelease: handleRelease,
    onSubtaskClick,
    onDelete: handleDelete,
    canDelete,
    deleteTooltip,
    isDeleting,
    branches,
    branchesLoading,
    branchesError,
    editingLocked,
    automationLockedReason,
    excludeStateFromPicker,
    forcedFeatureBranch,
    forcedCreatePR,
    clearForcedFeatureBranch: () => setForcedFeatureBranch(false),
    clearForcedCreatePR: () => setForcedCreatePR(false),
  });

  // If the active tab disappears (e.g. HITL ended, chat tab removed), fall
  // back to the resolved default. On mobile, `CardPanelBody` prepends a
  // synthetic `'card'` tab, so treat it as valid here; the body does its own
  // fallback. Without this check, a mobile default of `'card'` would be
  // considered "missing" from the desktop-built tab set and reset to
  // `'automation'`.
  const mobileAwareDefault: RailTabKey =
    isMobile && !isHITLRunning ? 'card' : resolvedDefaultTab;
  const effectiveTab =
    activeTab === 'card' || tabs.some((t) => t.key === activeTab)
      ? activeTab
      : mobileAwareDefault;

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={handleClose} />

      <div
        ref={panelRef}
        className="card-panel card-panel-bifold animate-panel-slide-in"
        role="dialog"
        aria-modal="true"
        aria-label="Card details"
      >
        <CardPanelHeader
          card={card}
          editedCard={editedCard}
          config={config}
          currentAgentId={currentAgentId}
          isDirty={isDirty}
          isSaving={isSaving}
          isDeleting={isDeleting}
          canRun={canRun}
          onClose={handleClose}
          onSave={handleSave}
          onTitleChange={(title) => setEditedCard((prev) => ({ ...prev, title }))}
          onPriorityChange={(priority) => setEditedCard((prev) => ({ ...prev, priority }))}
          onPrimaryAction={handlePrimary}
          onStopCard={onStopCard}
          onOpenDependency={onSubtaskClick}
          firstUnfinishedDep={firstUnfinishedDep}
        />

        <CardPanelBody
          left={
            <CardPanelLeft
              key={card.id}
              editedCard={editedCard}
              setEditedCard={setEditedCard}
              runnerAttached={runnerAttached}
              editingLocked={editingLocked}
              lockedReason={lockedReason}
              canToggleEditor={canToggleEditor}
            />
          }
          tabs={tabs}
          activeTab={effectiveTab}
          onTabChange={(tab) => {
            setSync((prev) => ({ ...prev, hitlOffCount: 0 }));
            setActiveTab(tab);
          }}
          railExpanded={railExpanded}
          onToggleRail={() => setRailExpanded((v) => !v)}
        />
      </div>

      <ConfirmModal
        open={confirmDiscardOpen}
        title="Discard unsaved changes?"
        message="Your edits to this card will be lost."
        confirmLabel="Discard"
        variant="danger"
        onConfirm={() => {
          setConfirmDiscardOpen(false);
          onClose();
        }}
        onCancel={() => setConfirmDiscardOpen(false)}
      />
    </>
  );
}
