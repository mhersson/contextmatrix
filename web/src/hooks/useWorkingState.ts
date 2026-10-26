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
 * optimistically when a send starts (instant feedback); the server's
 * assistant_working signal is the sole clearing authority, with the session
 * leaving `active` as the teardown catch-all. A stale `false` left in the
 * merged session view from the previous turn must not cancel a fresh
 * optimistic arm, so only evidence newer than the arm clears: a transition to
 * explicit false, or a false whose assistant_working_since differs from the
 * arm-time value (a turn ran, even if its true update was batched away).
 */
export function useWorkingState(
  sessionID: string,
  session: ChatSession | null,
): { working: WorkingState | null; armOptimistic: () => void; disarmOptimistic: () => void } {
  const [working, setWorking] = useState<WorkingState | null>(null);

  // Reset in render on session switch so the previous chat's state never
  // paints under the new one.
  const [prevSessionID, setPrevSessionID] = useState(sessionID);
  if (sessionID !== prevSessionID) {
    setPrevSessionID(sessionID);
    setWorking(null);
  }

  const assistantWorking = session?.assistant_working;
  const assistantWorkingSince = session?.assistant_working_since;
  const status = session?.status;

  const prevWorkingRef = useRef<boolean | undefined>(undefined);
  const latestRef = useRef({ assistantWorking, assistantWorkingSince });
  // undefined = not armed; null = armed with no turn seen yet.
  const armedSinceRef = useRef<string | null | undefined>(undefined);
  // The session the refs above describe; a switch resets them in the effect, as refs cannot change during render.
  const refsSessionRef = useRef(sessionID);

  useEffect(() => {
    latestRef.current = { assistantWorking, assistantWorkingSince };
  });

  // setState-in-effect is intentional: this effect derives `working` from
  // the previous-vs-current assistant_working comparison (prevWorkingRef),
  // which needs a committed previous render to compare against and so
  // cannot be expressed as an in-render state-marker reset like the
  // session-switch guard above.
  useEffect(() => {
    if (refsSessionRef.current !== sessionID) {
      refsSessionRef.current = sessionID;
      prevWorkingRef.current = undefined;
      armedSinceRef.current = undefined;
    }
    const prev = prevWorkingRef.current;
    prevWorkingRef.current = assistantWorking;

    if (status !== undefined && status !== 'active') {
      armedSinceRef.current = undefined;
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

    const turnRanSinceArm =
      armedSinceRef.current !== undefined && assistantWorkingSince !== (armedSinceRef.current ?? undefined);
    if (assistantWorking === false && (prev !== false || turnRanSinceArm)) {
      armedSinceRef.current = undefined;
      setWorking(null);
    }
  }, [sessionID, assistantWorking, assistantWorkingSince, status]);

  const armOptimistic = () => {
    armedSinceRef.current = latestRef.current.assistantWorkingSince ?? null;
    setWorking((w) => w ?? { verb: pickVerb(), since: Date.now() });
  };

  // A failed send must not discard a turn the server is already reporting.
  const disarmOptimistic = () => {
    armedSinceRef.current = undefined;
    if (latestRef.current.assistantWorking !== true) setWorking(null);
  };

  return { working, armOptimistic, disarmOptimistic };
}
