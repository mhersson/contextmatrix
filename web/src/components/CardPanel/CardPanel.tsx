import { useCallback, useRef, useState } from 'react';
import type { Card, LogEntry, ProjectConfig, PatchCardInput } from '../../types';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelBody, type RailTabKey } from './CardPanelBody';
import { CardPanelLeft } from './CardPanelLeft';
import { buildCardPanelTabs } from './buildCardPanelTabs';
import { isWorkerAttached, primaryAction } from './utils';
import { useCardEdits } from './useCardEdits';
import { useRailSync } from './useRailSync';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useBranches } from '../../hooks/useBranches';
import { useCardPanelKeyboard } from '../../hooks/useCardPanelKeyboard';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { useTheme } from '../../hooks/useTheme';
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
 * before firing the worker webhook, matching the server's force-enable
 * behavior when running a card.
 */
export function CardPanel(props: CardPanelProps) {
  const { card, config, cardLogs = [], onClose, onSave, onDelete, onClaim, onRelease,
    onSubtaskClick, currentAgentId, onRunCard, onStopCard } = props;

  const panelRef = useRef<HTMLDivElement>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  const {
    editedCard,
    setEditedCard,
    isDirty,
    isSaving,
    forcedFeatureBranch,
    forcedCreatePR,
    clearForcedFeatureBranch,
    clearForcedCreatePR,
    handleSave,
    handleRun,
    handleTransitionPrimary,
  } = useCardEdits(card, onSave, onRunCard);

  useFocusTrap(panelRef, true);

  const { taskBackend } = useTheme();

  const isMobile = useMediaQuery('(max-width: 768px)');
  // Chat is "live" (transcript streaming, tab shown with a pulse) whenever a
  // worker session is running — HITL or autonomous. Mirrors the
  // isCardChatLive predicate in ProjectShell.tsx — keep both in sync if the
  // liveness rule changes. Only a HITL session is *interactive*: it takes the
  // compose row, grabs the default tab, and auto-expands the rail. Autonomous
  // runs (plain or mob session) keep the tab available but read-only and unfocused.
  const isChatLive = card.worker_status === 'running';
  const isChatInteractive = isChatLive && !(card.autonomous ?? false);
  const defaultTab: RailTabKey = isChatInteractive ? 'chat' : isMobile ? 'card' : 'automation';

  const [confirmDiscardOpen, setConfirmDiscardOpen] = useState(false);

  const { railExpanded, setRailExpanded, activeTab, onTabChange } = useRailSync(
    card,
    isChatInteractive,
    defaultTab,
    setEditedCard,
  );

  const { branches, loading: branchesLoading, error: branchesError } =
    useBranches(card.project, !!taskBackend);

  const handleClaim = useCallback(async () => {
    // currentAgentId is the unified identity (session-derived in multi mode,
    // localStorage id in none mode). Null only while logged out behind the
    // gate — unreachable in practice; the guard is just type narrowing.
    if (!currentAgentId) return;
    await onClaim(currentAgentId);
  }, [currentAgentId, onClaim]);

  const handleRelease = useCallback(async () => {
    if (!card.assigned_agent) return;
    await onRelease(card.assigned_agent);
  }, [card.assigned_agent, onRelease]);

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
    !!taskBackend &&
    card.state === 'todo' &&
    (!card.worker_status || card.worker_status === 'failed' || card.worker_status === 'killed');

  const workerAttached = isWorkerAttached(card, currentAgentId);
  const primary = primaryAction(card, editedCard.autonomous ?? false, config, canRun);

  // First unfinished dep for the "Open dependency" helper (blocked cards).
  const firstUnfinishedDep =
    card.state === 'blocked' && card.depends_on && card.depends_on.length > 0
      ? card.depends_on[0]
      : null;

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
  // Worker-attached is automatic and clears when the worker releases.
  // State-driven asks the user to move the card back to `todo` — the only
  // state that can re-run. Both feed the left column (Labels) and the
  // Automation tab (checkbox rail), so compute once here.
  const isTodo = card.state === 'todo';
  const isSubtask = card.type === 'subtask';
  const stateLocksEditing = !isTodo && !workerAttached;
  const editingLocked = workerAttached || stateLocksEditing;
  const automationLocked = editingLocked || isSubtask;
  const lockedReason = workerAttached
    ? 'locked during remote run'
    : `locked outside todo · move card back to todo to edit (current: ${card.state.replace(/_/g, ' ')})`;
  const automationLockedReason = isSubtask
    ? 'Automation is managed on the parent card'
    : workerAttached
      ? 'Automation locked during remote run'
      : `Automation can only be edited in todo · current state: ${card.state.replace(/_/g, ' ')}`;
  const canToggleEditor =
    !workerAttached &&
    (card.state === 'todo' || card.state === 'done' || card.state === 'not_planned');

  const { tabs, defaultTab: resolvedDefaultTab } = buildCardPanelTabs({
    card,
    editedCard,
    setEditedCard,
    config,
    cardLogs,
    currentAgentId,
    workerAttached,
    isChatLive,
    isChatInteractive,
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
    automationLocked,
    automationLockedReason,
    excludeStateFromPicker,
    forcedFeatureBranch,
    forcedCreatePR,
    clearForcedFeatureBranch,
    clearForcedCreatePR,
  });

  // If the active tab disappears (e.g. live session ended, chat tab removed),
  // fall back to the resolved default. On mobile, `CardPanelBody` prepends a
  // synthetic `'card'` tab, so treat it as valid here; the body does its own
  // fallback. Without this check, a mobile default of `'card'` would be
  // considered "missing" from the desktop-built tab set and reset to
  // `'automation'`.
  const mobileAwareDefault: RailTabKey =
    isMobile && !isChatInteractive ? 'card' : resolvedDefaultTab;
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
          onTypeChange={(type) => setEditedCard((prev) => ({ ...prev, type }))}
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
              workerAttached={workerAttached}
              editingLocked={editingLocked}
              lockedReason={lockedReason}
              canToggleEditor={canToggleEditor}
            />
          }
          tabs={tabs}
          activeTab={effectiveTab}
          onTabChange={onTabChange}
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
