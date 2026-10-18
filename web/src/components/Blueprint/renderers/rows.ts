import type { Assumption } from '../artifactData';
import { changedById } from '../revisions';

export interface NumberedAssumption {
  a: Assumption;
  /** The assumption's place on the sheet, from 1. */
  n: number;
  /** The id, or the text when the stage gave none; a repeat gets its occurrence appended. */
  key: string;
}

export function numbered(items: Assumption[]): NumberedAssumption[] {
  const seen = new Map<string, number>();
  return items.map((a, i) => {
    const base = a.id || a.text;
    const count = (seen.get(base) ?? 0) + 1;
    seen.set(base, count);
    return { a, n: i + 1, key: count === 1 ? base : `${base}#${count}` };
  });
}

export function assumptionKey(row: NumberedAssumption): string {
  return row.key;
}

/** Keys of the assumptions new or changed since `previous`; none without one. */
export function changedAssumptions(previous: Assumption[] | null, items: Assumption[]): Set<string> {
  if (!previous) return new Set();
  const keyed = (xs: Assumption[]) => numbered(xs).map((row) => ({ ...row.a, id: row.key }));
  return changedById(keyed(previous), keyed(items));
}
