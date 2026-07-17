import { useCallback, useState } from 'react';
import type React from 'react';
import type { Card } from '../../types';
import type { RailTabKey } from './CardPanelBody';
import { safeReadBool, safeWriteBool } from '../../utils/safeStorage';

const RAIL_STORAGE_KEY = 'contextmatrix-rail-expanded';

const safeReadRail = () => safeReadBool(RAIL_STORAGE_KEY);
const safeWriteRail = (value: boolean) => safeWriteBool(RAIL_STORAGE_KEY, value);

export interface RailSync {
  railExpanded: boolean;
  setRailExpanded: React.Dispatch<React.SetStateAction<boolean>>;
  activeTab: RailTabKey;
  onTabChange: (tab: RailTabKey) => void;
}

/**
 * Manages rail layout state (railExpanded, activeTab) with the documented
 * sync state machine that reacts to card-identity changes and interactive-chat
 * transitions (a HITL session running - autonomous runs stream a read-only
 * chat tab but never grab focus, so they do not participate here).
 *
 * State machine summary:
 *
 *  - Card identity change (cardId changes): full reset - editedCard,
 *    railExpanded → safeReadRail() ?? isChatInteractive, activeTab → defaultTab.
 *  - Same card, new SSE object reference: editedCard refreshes; railExpanded
 *    and activeTab are preserved.
 *  - isChatInteractive flip to true: resets activeTab → 'chat', railExpanded →
 *    true (and persists true to localStorage).
 *  - isChatInteractive flip to false: ARMS the debounce; after two further
 *    consecutive renders still observing false it fires once, switching
 *    activeTab back to defaultTab (only if the user is still on 'chat').
 *    Disarmed by flip-to-true, card-id change, or user-initiated tab change.
 *    Arming strictly on the flip matters: SSE card refreshes arrive
 *    constantly during a run, and a counter that increments on every
 *    refresh while non-interactive would repeatedly kick the user off the
 *    read-only chat of a running autonomous session.
 *
 * The state machine runs in-render (not useEffect) so resets are synchronous
 * with the prop change. The debounce counter lives in the sync state object
 * (not a useRef) to comply with the react-hooks/refs lint rule.
 *
 * railExpanded is persisted to localStorage under RAIL_STORAGE_KEY so the
 * expanded/collapsed preference survives view-switching (chat, AllProjects)
 * that unmounts CardPanel, and page reloads.
 */
export function useRailSync(
  card: Card,
  isChatInteractive: boolean,
  defaultTab: RailTabKey,
  setEditedCard: React.Dispatch<React.SetStateAction<Card>>,
): RailSync {
  const [railExpanded, setRailExpandedRaw] = useState<boolean>(
    () => safeReadRail() ?? isChatInteractive,
  );
  const [activeTab, setActiveTab] = useState<RailTabKey>(defaultTab);
  const [sync, setSync] = useState({
    cardId: card.id,
    card,
    isChatInteractive,
    liveOffCount: 0,
    armed: false,
  });

  // Wrapped setter that persists every change to localStorage.
  // Stable reference (dep array []) because setRailExpandedRaw is a stable
  // React dispatch and safeWriteRail is a module-level function.
  const setRailExpanded: React.Dispatch<React.SetStateAction<boolean>> = useCallback(
    (action: React.SetStateAction<boolean>) => {
      setRailExpandedRaw((prev) => {
        const next = typeof action === 'function' ? action(prev) : action;
        safeWriteRail(next);
        return next;
      });
    },
    [],
  );

  // In-render state machine - must not be moved to useEffect.
  if (sync.cardId !== card.id) {
    // Card identity changed: full reset. Re-read from localStorage rather than
    // using the in-memory railExpanded value: another tab may have written a
    // different preference since this tab last changed it, and reading here
    // ensures we pick up that concurrent write instead of clobbering it.
    const restoredExpanded = safeReadRail() ?? isChatInteractive;
    setSync({ cardId: card.id, card, isChatInteractive, liveOffCount: 0, armed: false });
    setEditedCard(card);
    setRailExpandedRaw(restoredExpanded);
    setActiveTab(defaultTab);
  } else if (sync.card !== card || sync.isChatInteractive !== isChatInteractive) {
    const flippedOn = sync.isChatInteractive !== isChatInteractive && isChatInteractive;
    const flippedOff = sync.isChatInteractive && !isChatInteractive;
    if (sync.card !== card) setEditedCard(card);
    if (flippedOn) {
      // Interactive chat flipped live: jump to chat tab, expand rail, disarm.
      // Persist the forced-expand so it survives remounts.
      safeWriteRail(true);
      setSync({ cardId: card.id, card, isChatInteractive, liveOffCount: 0, armed: false });
      setActiveTab('chat');
      setRailExpandedRaw(true);
    } else {
      // A true→false flip arms the debounce; only while armed do further
      // renders count. Firing (or reaching the threshold off the chat tab)
      // disarms, so ordinary SSE card refreshes during a never-interactive
      // (autonomous) session can never yank the user off the chat tab.
      const armed = flippedOff || sync.armed;
      const nextCount = armed && !flippedOff ? sync.liveOffCount + 1 : 0;
      const done = armed && nextCount >= 2;
      setSync({
        cardId: card.id,
        card,
        isChatInteractive,
        liveOffCount: nextCount,
        armed: armed && !done,
      });
      if (done && activeTab === 'chat') {
        setActiveTab(defaultTab);
      }
    }
  }

  const onTabChange = (tab: RailTabKey) => {
    setActiveTab(tab);
    // Disarm the live-off debounce on any user-initiated tab change - their
    // choice of tab wins over the pending switch-back.
    setSync((prev) => ({ ...prev, liveOffCount: 0, armed: false }));
  };

  return {
    railExpanded,
    setRailExpanded,
    activeTab,
    onTabChange,
  };
}
