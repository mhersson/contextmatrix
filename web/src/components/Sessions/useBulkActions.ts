import { useState } from 'react';
import { useToast } from '../../hooks/useToast';

/** Runs `act` on every id at once, then `refresh` once; resolves to the ids it failed on. */
export async function settleEach(
  ids: string[],
  act: (id: string) => Promise<unknown>,
  refresh: () => void,
): Promise<string[]> {
  const results = await Promise.allSettled(ids.map(act));
  refresh();
  return ids.filter((_, i) => results[i].status === 'rejected');
}

interface TickedRows {
  selected: string[];
  tickOnly: (ids: string[]) => void;
}

/** The bulk bar's actions on the ticked rows: one busy flag for the bar, and a delete that keeps its failures ticked. */
export function useBulkActions({ selected, tickOnly }: TickedRows) {
  const { showToast } = useToast();
  const [busy, setBusy] = useState(false);

  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    try {
      await action();
    } finally {
      setBusy(false);
    }
  };

  const deleteTicked = (deleteOne: (id: string) => Promise<void>, refresh: () => void, noun: (n: number) => string) =>
    run(async () => {
      const failed = await settleEach(selected, deleteOne, refresh);
      tickOnly(failed);
      if (failed.length > 0) showToast(`Could not delete ${noun(failed.length)}`, 'error');
    });

  return { busy, run, deleteTicked };
}
