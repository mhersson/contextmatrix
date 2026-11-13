import { describe, it, expect } from 'vitest';
import type { Card } from '../../types';
import { buildCardPanelTabs } from './buildCardPanelTabs';

function keys(sentinelOn: boolean) {
  const card = { id: 'T-001', project: 'p', type: 'task' } as Card;
  const noop = async () => undefined;
  const { tabs } = buildCardPanelTabs({
    card,
    editedCard: card,
    setEditedCard: () => undefined,
    config: {} as never,
    cardLogs: [],
    currentAgentId: null,
    workerAttached: false,
    cards: [],
    isChatLive: false,
    isChatInteractive: false,
    onClaim: noop,
    onRelease: noop,
    onSubtaskClick: () => undefined,
    onDelete: noop,
    canDelete: true,
    deleteTooltip: '',
    isDeleting: false,
    onForceRelease: noop,
    canForceRelease: false,
    isForceReleasing: false,
    branches: [],
    branchesLoading: false,
    branchesError: false,
    automationLocked: false,
    automationLockedReason: '',
    excludeStateFromPicker: null,
    onDependsOnChange: noop,
    sentinelOn,
  });
  return tabs.map((t) => t.key);
}

describe('buildCardPanelTabs', () => {
  it('puts Sentinel after Info and before Danger only while Sentinel is on', () => {
    expect(keys(true)).toEqual(['automation', 'info', 'sentinel', 'danger']);
    expect(keys(false)).toEqual(['automation', 'info', 'danger']);
  });
});
