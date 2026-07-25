import type { LogEntry } from '../../types';

const EMPTY: readonly LogEntry[] = [];

/**
 * Wholesale gate for the card-scoped transcript: returns `logs` unchanged
 * (same reference - no per-render allocation) only when every entry that
 * carries a card_id belongs to `cardId`; otherwise returns a shared empty
 * array. The ring buffer holds exactly one stream, so a foreign entry means
 * the whole buffer is stale (the identity-change clear in useWorkerLogs runs
 * post-paint, so the first render after a direct card switch still sees the
 * old snapshot); client gap markers (card_id === '') from a foreign stream
 * are dropped along with it.
 */
export function gateCardLogs(
  logs: readonly LogEntry[],
  cardId: string | undefined,
): readonly LogEntry[] {
  if (!cardId || logs.length === 0) return EMPTY;
  for (const entry of logs) {
    if (entry.card_id !== '' && entry.card_id !== cardId) return EMPTY;
  }
  return logs;
}
