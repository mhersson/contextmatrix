import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { Card } from '../../types';

interface UseDeepLinkCardArgs {
  /** Cards currently loaded for the active project. */
  cards: Card[];
  /** True while the board is fetching cards for the active project. */
  loading: boolean;
  /** Currently selected card (or null). */
  selectedCard: Card | null;
  /** Setter for the selected card. */
  setSelectedCard: (card: Card | null) => void;
  /** Active project name from the URL. Used to reset state on cross-project SPA nav. */
  project: string | undefined;
}

/**
 * Deep-link protocol for `?card=ID` query param.
 *
 * Inbound: when `?card=ID` is in the URL and the matching card has loaded,
 * open the CardPanel (set `selectedCard`).
 *
 * Dead-link: when `?card=ID` is set but no matching card exists in the loaded
 * board (not the same as still-loading - we wait for `loading=false`), strip
 * the URL once so the dead link does not interfere with future interactions.
 *
 * Outbound: when a consumed deep-link panel closes, strip `?card=` from the
 * URL (the user closed the deep-linked panel).
 *
 * Click-driven panel opens deliberately do NOT write to the URL - full
 * URL ↔ panel sync is a deferred follow-up.
 *
 * State is reset on project change so a cross-project SPA navigation
 * (`/projects/A?card=A-1` → `/projects/B?card=B-2`) re-fires the inbound
 * branch for the second project.
 *
 * All branches are implemented as in-render state-marker patterns rather
 * than reactive effects because `react-hooks/set-state-in-effect` forbids
 * calling setState from useEffect. The actual `setSearchParams` mutation
 * (router state = external system) runs in a useEffect.
 */
export function useDeepLinkCard({
  cards,
  loading,
  selectedCard,
  setSelectedCard,
  project,
}: UseDeepLinkCardArgs): void {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlCardId = searchParams.get('card');

  // Reset deep-link state when the project changes so cross-project SPA
  // navigation re-fires the inbound branch for the new project.
  const [prevProject, setPrevProject] = useState(project);
  const [deepLinkConsumed, setDeepLinkConsumed] = useState(false);
  const [deadLinkProcessed, setDeadLinkProcessed] = useState<string | null>(null);
  const [pendingUrlStrip, setPendingUrlStrip] = useState(false);

  if (project !== prevProject) {
    setPrevProject(project);
    setDeepLinkConsumed(false);
    setDeadLinkProcessed(null);
    setPendingUrlStrip(false);
  }

  // Inbound: URL has ?card=ID and not yet consumed → try to open the card.
  if (urlCardId && !deepLinkConsumed && selectedCard?.id !== urlCardId) {
    const card = cards.find((c) => c.id === urlCardId);
    if (card) {
      setSelectedCard(card);
      setDeepLinkConsumed(true);
    }
  }

  // Dead-link: URL has ?card=ID, board has finished loading, no match exists,
  // and we have not yet processed this exact (project, urlCardId) pair.
  // Strip the URL once so the dead link does not stay forever.
  if (
    urlCardId &&
    !loading &&
    !deepLinkConsumed &&
    deadLinkProcessed !== urlCardId &&
    !cards.some((c) => c.id === urlCardId)
  ) {
    setDeadLinkProcessed(urlCardId);
    setPendingUrlStrip(true);
  }

  // Outbound: deep-linked panel closed → strip ?card= from URL.
  if (deepLinkConsumed && selectedCard === null && urlCardId && !pendingUrlStrip) {
    setPendingUrlStrip(true);
    setDeepLinkConsumed(false);
  }

  // Self-clean: ?card= has been removed → clear strip + dead-link bookkeeping
  // so a future re-entry of the same dead link triggers a fresh strip.
  if (pendingUrlStrip && !urlCardId) {
    setPendingUrlStrip(false);
  }
  if (deadLinkProcessed !== null && !urlCardId) {
    setDeadLinkProcessed(null);
  }

  useEffect(() => {
    if (!pendingUrlStrip) return;
    setSearchParams(
      (p) => {
        p.delete('card');
        return p;
      },
      { replace: true },
    );
  }, [pendingUrlStrip, setSearchParams]);
}
