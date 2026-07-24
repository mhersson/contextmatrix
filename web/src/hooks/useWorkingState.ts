import { useEffect, useRef, useState } from 'react';
import type { ChatSession } from '../types';

export interface WorkingState {
  verb: string;
  /** Epoch millis the in-flight turn started (server time once known). */
  since: number;
}

/**
 * Whimsical gerunds shown while the assistant works. One is picked at random
 * per turn and stays fixed for that turn. Lives here rather than in the
 * component because the hook is the only consumer - WorkingIndicator receives
 * the chosen verb as a prop.
 */
export const WORKING_VERBS = [
  "Beboppin'",
  'Noodling',
  'Percolating',
  'Ruminating',
  'Tinkering',
  'Mulling',
  'Brewing',
  'Wrangling',
  'Simmering',
  'Pondering',
  'Cogitating',
  'Conjuring',
  'Marinating',
  'Whirring',
  'Doodling',
  'Scheming',
] as const;

function pickVerb(): string {
  return WORKING_VERBS[Math.floor(Math.random() * WORKING_VERBS.length)];
}

/**
 * Owns the working-indicator state machine for one chat session. Arms
 * optimistically when a send resolves (instant feedback); the server's
 * assistant_working signal is the sole clearing authority, with the session
 * leaving `active` as the teardown catch-all. A stale `false` left in the
 * merged session view from the previous turn must not cancel a fresh
 * optimistic arm, so only a fresh transition to explicit false clears
 * (mirrors the prevStatusRef idiom in useChatStream).
 */
export function useWorkingState(
  sessionID: string,
  session: ChatSession | null,
): { working: WorkingState | null; armOptimistic: () => void } {
  const [working, setWorking] = useState<WorkingState | null>(null);

  // Reset synchronously on session switch - see web/CLAUDE.md § CardPanel
  // for why this lives in render, not useEffect.
  const [prevSessionID, setPrevSessionID] = useState(sessionID);
  if (sessionID !== prevSessionID) {
    setPrevSessionID(sessionID);
    setWorking(null);
  }

  const assistantWorking = session?.assistant_working;
  const assistantWorkingSince = session?.assistant_working_since;
  const status = session?.status;

  const prevWorkingRef = useRef<boolean | undefined>(undefined);

  // setState-in-effect is intentional: this effect derives `working` from
  // the previous-vs-current assistant_working comparison (prevWorkingRef),
  // which needs a committed previous render to compare against and so
  // cannot be expressed as an in-render state-marker reset like the
  // session-switch guard above.
  useEffect(() => {
    const prev = prevWorkingRef.current;
    prevWorkingRef.current = assistantWorking;

    if (status !== undefined && status !== 'active') {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setWorking(null);
      return;
    }

    if (assistantWorking === true) {
      const parsed = assistantWorkingSince ? Date.parse(assistantWorkingSince) : NaN;
      setWorking((w) => ({
        verb: w?.verb ?? pickVerb(),
        since: Number.isFinite(parsed) ? parsed : (w?.since ?? Date.now()),
      }));
      return;
    }

    if (assistantWorking === false && prev !== false) {
      setWorking(null);
    }
  }, [assistantWorking, assistantWorkingSince, status]);

  const armOptimistic = () => {
    setWorking((w) => w ?? { verb: pickVerb(), since: Date.now() });
  };

  return { working, armOptimistic };
}
