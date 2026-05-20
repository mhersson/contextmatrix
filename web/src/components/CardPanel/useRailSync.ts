import { useState } from 'react';
import type React from 'react';
import type { Card } from '../../types';
import type { RailTabKey } from './CardPanelBody';

export interface RailSync {
  railExpanded: boolean;
  setRailExpanded: React.Dispatch<React.SetStateAction<boolean>>;
  activeTab: RailTabKey;
  onTabChange: (tab: RailTabKey) => void;
}

/**
 * Manages rail layout state (railExpanded, activeTab) with the documented
 * sync state machine that reacts to card-identity changes and HITL
 * transitions.
 *
 * State machine summary (from web/CLAUDE.md "Rail tabs + default tab on HITL"):
 *
 *  - Card identity change (cardId changes): full reset — editedCard,
 *    railExpanded → isHITLRunning, activeTab → defaultTab.
 *  - Same card, new SSE object reference: editedCard refreshes; railExpanded
 *    and activeTab are preserved.
 *  - isHITLRunning flip to true: resets activeTab → 'chat', railExpanded → true.
 *  - isHITLRunning flip to false: waits for two consecutive renders both
 *    observing false before switching activeTab back to defaultTab.
 *    Counter resets on HITL-on flip, card-id change, or user-initiated tab change.
 *
 * The state machine runs in-render (not useEffect) so resets are synchronous
 * with the prop change. The debounce counter lives in the sync state object
 * (not a useRef) to comply with the react-hooks/refs lint rule.
 */
export function useRailSync(
  card: Card,
  isHITLRunning: boolean,
  defaultTab: RailTabKey,
  setEditedCard: React.Dispatch<React.SetStateAction<Card>>,
): RailSync {
  const [railExpanded, setRailExpanded] = useState(isHITLRunning);
  const [activeTab, setActiveTab] = useState<RailTabKey>(defaultTab);
  const [sync, setSync] = useState({
    cardId: card.id,
    card,
    isHITLRunning,
    hitlOffCount: 0,
  });

  // In-render state machine — must not be moved to useEffect.
  if (sync.cardId !== card.id) {
    // Card identity changed: full reset.
    setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: 0 });
    setEditedCard(card);
    setRailExpanded(isHITLRunning);
    setActiveTab(defaultTab);
  } else if (sync.card !== card || sync.isHITLRunning !== isHITLRunning) {
    const hitlFlippedOn = sync.isHITLRunning !== isHITLRunning && isHITLRunning;
    if (sync.card !== card) setEditedCard(card);
    if (hitlFlippedOn) {
      // HITL flipped on: jump to chat tab, expand rail, reset counter.
      setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: 0 });
      setActiveTab('chat');
      setRailExpanded(true);
    } else {
      // HITL flipped off or stayed off. Increment the debounce counter;
      // switch the tab only when the counter reaches 2.
      const nextCount =
        !isHITLRunning && !sync.isHITLRunning ? sync.hitlOffCount + 1 : 0;
      setSync({ cardId: card.id, card, isHITLRunning, hitlOffCount: nextCount });
      if (nextCount >= 2) {
        setActiveTab(defaultTab);
      }
    }
  }

  const onTabChange = (tab: RailTabKey) => {
    setActiveTab(tab);
    // Reset the HITL-off debounce counter on any user-initiated tab change
    // so a subsequent HITL-off flip starts a fresh count.
    setSync((prev) => ({ ...prev, hitlOffCount: 0 }));
  };

  return {
    railExpanded,
    setRailExpanded,
    activeTab,
    onTabChange,
  };
}
