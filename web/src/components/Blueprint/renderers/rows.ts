import type { Assumption } from '../artifactData';
import { changedById } from '../revisions';

export interface NumberedAssumption {
  a: Assumption;
  /** The assumption's place on the sheet, from 1. */
  n: number;
}

export function numbered(items: Assumption[]): NumberedAssumption[] {
  return items.map((a, i) => ({ a, n: i + 1 }));
}

/** An assumption's id, or its text when the stage gave it none. */
export function assumptionKey(row: NumberedAssumption): string {
  return row.a.id || row.a.text;
}

/** Keys of the assumptions new or changed since `previous`; none without one. */
export function changedAssumptions(previous: Assumption[] | null, items: Assumption[]): Set<string> {
  if (!previous) return new Set();
  const keyed = (xs: Assumption[]) => numbered(xs).map((row) => ({ ...row.a, id: assumptionKey(row) }));
  return changedById(keyed(previous), keyed(items));
}
