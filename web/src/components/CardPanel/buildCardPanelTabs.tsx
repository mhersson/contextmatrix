import type { Dispatch, SetStateAction } from 'react';
import type { Card, LogEntry, ProjectConfig } from '../../types';
import type { RailTab, RailTabKey } from './CardPanelBody';
import { AutomationTab } from './tabs/AutomationTab';
import { ChatTab } from './tabs/ChatTab';
import { DangerTab } from './tabs/DangerTab';
import { InfoTab } from './tabs/InfoTab';

interface BuildCardPanelTabsOptions {
  card: Card;
  editedCard: Card;
  setEditedCard: Dispatch<SetStateAction<Card>>;
  config: ProjectConfig;
  cardLogs: readonly LogEntry[];
  currentAgentId: string | null;
  runnerAttached: boolean;
  isHITLRunning: boolean;
  onClaim: () => Promise<void>;
  onRelease: () => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  onDelete: () => Promise<void>;
  canDelete: boolean;
  deleteTooltip: string;
  isDeleting: boolean;
  branches: string[];
  branchesLoading: boolean;
  branchesError: boolean;
  automationLocked: boolean;
  automationLockedReason: string;
  excludeStateFromPicker: string | null;
  forcedFeatureBranch: boolean;
  forcedCreatePR: boolean;
  clearForcedFeatureBranch: () => void;
  clearForcedCreatePR: () => void;
}

/**
 * Assembles the rail tab registry. Each tab's `content` is a dedicated
 * component so the JSX tree stays shallow and each tab can be updated or
 * memoised independently. The chat tab is pushed whenever a transcript
 * exists (live HITL or replayed history) so the conversation stays
 * accessible after the session ends or the card is promoted to autonomous.
 * The pulse indicator is only rendered while HITL is live; `defaultTab`
 * still flips to chat only on a live session so freshly opening a
 * finalized card lands on Automation by default.
 *
 * Not a hook: no state, no effects — a pure builder. Named `buildCardPanelTabs`
 * so React/ESLint hook rules and readers don't mistake it for one.
 */
export function buildCardPanelTabs(opts: BuildCardPanelTabsOptions): {
  tabs: RailTab[];
  defaultTab: RailTabKey;
} {
  const tabs: RailTab[] = [];
  const isSubtask = opts.card.type === 'subtask';

  if (!isSubtask && (opts.isHITLRunning || opts.cardLogs.length > 0)) {
    tabs.push({
      key: 'chat',
      label: 'Chat',
      indicator: opts.isHITLRunning ? (
        <span
          className="inline-block w-2 h-2 rounded-full animate-pulse"
          style={{ backgroundColor: 'var(--aqua)' }}
          aria-hidden="true"
        />
      ) : undefined,
      content: <ChatTab card={opts.card} cardLogs={opts.cardLogs} />,
    });
  }

  tabs.push({
    key: 'automation',
    label: 'Automation',
    content: (
      <AutomationTab
        card={opts.card}
        editedCard={opts.editedCard}
        setEditedCard={opts.setEditedCard}
        branches={opts.branches}
        branchesLoading={opts.branchesLoading}
        branchesError={opts.branchesError}
        editingLocked={opts.automationLocked}
        automationLockedReason={opts.automationLockedReason}
        forcedFeatureBranch={opts.forcedFeatureBranch}
        forcedCreatePR={opts.forcedCreatePR}
        clearForcedFeatureBranch={opts.clearForcedFeatureBranch}
        clearForcedCreatePR={opts.clearForcedCreatePR}
      />
    ),
  });

  tabs.push({
    key: 'info',
    label: 'Info',
    content: (
      <InfoTab
        card={opts.card}
        editedCard={opts.editedCard}
        setEditedCard={opts.setEditedCard}
        config={opts.config}
        currentAgentId={opts.currentAgentId}
        runnerAttached={opts.runnerAttached}
        onSubtaskClick={opts.onSubtaskClick}
        onClaim={opts.onClaim}
        onRelease={opts.onRelease}
        excludeStateFromPicker={opts.excludeStateFromPicker}
      />
    ),
  });

  tabs.push({
    key: 'danger',
    label: 'Danger',
    indicator: <span aria-hidden="true">⚠</span>,
    content: (
      <DangerTab
        card={opts.card}
        canDelete={opts.canDelete}
        deleteTooltip={opts.deleteTooltip}
        isDeleting={opts.isDeleting}
        onDelete={opts.onDelete}
      />
    ),
  });

  const defaultTab: RailTabKey = opts.isHITLRunning ? 'chat' : 'automation';

  return { tabs, defaultTab };
}
