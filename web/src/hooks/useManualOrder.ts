import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Card } from '../types';
import { isTerminalState } from '../lib/cardState';
import { safeGetJSON, safeSetJSON } from '../utils/safeStorage';

export interface ManualOrder {
  getOrder: (state: string) => string[];
  hasOrder: (state: string) => boolean;
  // movedId/movedIdState let a caller judge one id against its post-move
  // state instead of the (possibly stale) state in the drag-time `cards`
  // snapshot - see prune()'s `override` param.
  setOrder: (state: string, ids: string[], movedId?: string, movedIdState?: string) => void;
}

type RawOrderRecord = Record<string, unknown>;
type OrderRecord = Record<string, string[]>;

function storageKey(project: string): string {
  return `contextmatrix-manual-order-${project}`;
}

function loadRawRecord(project: string): RawOrderRecord {
  const stored = safeGetJSON<unknown>(storageKey(project));
  if (!stored || typeof stored !== 'object' || Array.isArray(stored)) return {};

  return stored as RawOrderRecord;
}

/**
 * Structural pruning (unknown state keys, non-array values, non-string
 * entries, duplicate ids) always runs, so a hand-edited or corrupt stored
 * value can never reach a caller unvalidated - even before the board has
 * loaded. Card-membership pruning - an id with no matching card, and an id
 * whose card has reached a terminal state under a different column key - is
 * gated on `hasBoard`: while `cards` is empty the board is still loading, and
 * every id would otherwise look orphaned and get wiped. Never prunes by plain
 * column membership - a card that moves `todo` -> `in_progress` -> `todo`
 * must return to its slot, and a cross-column drop records a position before
 * the state change lands.
 *
 * `override` lets a caller judge one specific id (the card a drag just
 * moved) against its post-move state rather than whatever `cards` - a
 * drag-time snapshot that can be stale by the time an async write actually
 * runs - says it is. Without it, a drop out of a terminal state into a
 * manual column is judged against the still-terminal drag-time state and the
 * id is silently dropped from the order it was just placed in.
 *
 * Built with `Object.create(null)` rather than `{}`: `result` is keyed by
 * board state names, which are operator-defined and unvalidated against
 * `Object.prototype` member names. A plain object would let a state named
 * `constructor` (etc.) shadow-read as a function instead of `undefined`, and
 * a state named `__proto__` would hit the exotic proto-setter on assignment
 * instead of becoming an own property.
 */
function prune(
  raw: RawOrderRecord,
  cards: Card[],
  states: string[],
  override?: { id: string; state: string },
): OrderRecord {
  const validStates = new Set(states);
  const cardStateById = new Map(cards.map((card) => [card.id, card.state]));
  if (override) cardStateById.set(override.id, override.state);
  const hasBoard = cards.length > 0;

  const result: OrderRecord = Object.create(null) as OrderRecord;
  for (const [state, value] of Object.entries(raw)) {
    if (!validStates.has(state) || !Array.isArray(value)) continue;

    const seen = new Set<string>();
    const ids: string[] = [];
    for (const entry of value) {
      if (typeof entry !== 'string' || seen.has(entry)) continue;
      seen.add(entry);

      if (hasBoard) {
        const cardState = cardStateById.get(entry);
        if (cardState === undefined) continue;
        if (isTerminalState(cardState) && cardState !== state) continue;
      }

      ids.push(entry);
    }

    result[state] = ids;
  }

  return result;
}

/**
 * True when the pruned `record` has exactly the same keys, in any order, and
 * each key's array has the same entries in the same order as `raw`. Used to
 * decide whether a prune actually changed anything worth persisting.
 */
function recordsEqual(a: RawOrderRecord, b: OrderRecord): boolean {
  const bKeys = Object.keys(b);
  if (Object.keys(a).length !== bKeys.length) return false;

  for (const key of bKeys) {
    const aValue = a[key];
    const bValue = b[key];
    if (!Array.isArray(aValue) || aValue.length !== bValue.length) return false;
    for (let i = 0; i < bValue.length; i++) {
      if (aValue[i] !== bValue[i]) return false;
    }
  }

  return true;
}

/**
 * Per-project manual card ordering persisted to localStorage. Storage and
 * pruning only - the drag-drop ordering math lives in `manualOrder.applyMove`.
 */
export function useManualOrder(project: string, cards: Card[], states: string[]): ManualOrder {
  // Track [project, record] together so a project change is detected during
  // render and the stored record for the new project is returned immediately,
  // without an extra useEffect round-trip. Mirrors useColumnSort.
  const [state, setState] = useState<{ project: string; record: RawOrderRecord }>(() => ({
    project,
    record: loadRawRecord(project),
  }));

  let raw = state.record;
  if (state.project !== project) {
    raw = loadRawRecord(project);
    setState({ project, record: raw });
  }

  const record = useMemo(() => prune(raw, cards, states), [raw, cards, states]);

  // `setOrder` writes stay outside the `setState` updater (see below), so it
  // cannot read a guaranteed-fresh `record` from there. `recordRef` stands in:
  // synced after every commit and again inside `setOrder` itself (for two
  // calls in the same tick, before any commit happens), its `.current` is
  // always the latest merge base regardless of which render's `setOrder`
  // closure is invoked or how long a caller sits on a reference before
  // calling it (e.g. a cross-column write deferred behind a PATCH
  // round-trip). Without it, an older `setOrder` call landing after a newer
  // one reverts the newer one instead of merging on top of it. Synced in an
  // effect rather than during render - refs are for use outside render.
  // Tagged with the project it belongs to: `setOrder` reads the ref live but
  // closes over `project`/`cards`/`states`, so a write deferred across a
  // project switch would otherwise merge the new project's record and persist
  // it under the old project's key, where the membership prune empties it.
  const recordRef = useRef({ project, record });
  useEffect(() => {
    recordRef.current = { project, record };
  }, [project, record]);

  // The prune above is a derived view over `raw`; on its own it would reverse
  // the moment the source data changes back, so a card returning from a
  // terminal state would be silently re-admitted to its old slot instead of
  // treated as new (Design decision 6). Persisting the pruned result here
  // makes leaving the order a one-way event: the id is gone from storage and
  // from `raw` itself, so a later prune has nothing left to re-admit. This
  // cannot loop - once it runs, `raw` and `record` converge, so the next
  // render's `recordsEqual` check short-circuits. setState-in-effect is
  // intentional: syncing storage with a value only known after render is
  // exactly what this effect is for.
  useEffect(() => {
    if (recordsEqual(raw, record)) return;
    safeSetJSON(storageKey(project), record);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setState({ project, record });
  }, [project, raw, record]);

  const getOrder = useCallback((column: string): string[] => record[column] ?? [], [record]);

  const hasOrder = useCallback(
    (column: string): boolean => (record[column]?.length ?? 0) > 0,
    [record],
  );

  const setOrder = useCallback(
    (column: string, ids: string[], movedId?: string, movedIdState?: string) => {
      // Merge onto the ref only when it belongs to this closure's project;
      // after a switch it holds the other project's record, so re-read this
      // project's stored one rather than dropping its other columns.
      const base =
        recordRef.current.project === project
          ? recordRef.current.record
          : loadRawRecord(project);
      const merged = { ...base, [column]: ids };
      const override = movedId ? { id: movedId, state: movedIdState ?? column } : undefined;
      const next = prune(merged, cards, states, override);
      safeSetJSON(storageKey(project), next);
      // Kept in sync with the effect above, but set here too so a second
      // setOrder call in the same tick - before that effect has run - merges
      // on top of this write instead of the stale value.
      recordRef.current = { project, record: next };
      setState({ project, record: next });
    },
    [project, cards, states],
  );

  return { getOrder, hasOrder, setOrder };
}
