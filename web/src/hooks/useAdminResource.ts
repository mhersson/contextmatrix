import { useCallback, useEffect, useState } from 'react';
import { errorMessage } from '../lib/errors';

interface AdminResource<T> {
  items: T;
  loading: boolean;
  listError: string | null;
  actionError: string | null;
  setActionError: (message: string | null) => void;
  refetch: () => Promise<void>;
  act: (fn: () => Promise<unknown>, failMessage: string) => Promise<void>;
}

/**
 * Fetch/refetch/action plumbing shared by the admin CRUD pages: mount-only
 * load, separate list vs action errors, and act() for the common
 * mutate-then-refetch shape. Bespoke actions (busy flags, modal-scoped
 * errors, deliberate no-refetch) stay in the pages and use setActionError
 * directly. `fetcher` must be referentially stable - a module-level wrapper
 * around the api method - or the mount effect refires on every render.
 */
export function useAdminResource<T>(
  fetcher: () => Promise<T>,
  initial: T,
  loadFailMessage: string,
): AdminResource<T> {
  const [items, setItems] = useState<T>(initial);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const refetch = useCallback(async () => {
    try {
      const result = await fetcher();
      setItems(result);
      setListError(null);
    } catch (err) {
      setListError(errorMessage(err, loadFailMessage));
    } finally {
      setLoading(false);
    }
  }, [fetcher, loadFailMessage]);

  // Mount-only fetch, delegated to refetch (also reused after mutations).
  // setState-in-effect is intentional: this effect's whole purpose is to
  // trigger the initial load.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refetch();
  }, [refetch]);

  const act = useCallback(
    async (fn: () => Promise<unknown>, failMessage: string) => {
      setActionError(null);
      try {
        await fn();
        await refetch();
      } catch (err) {
        setActionError(errorMessage(err, failMessage));
      }
    },
    [refetch],
  );

  return { items, loading, listError, actionError, setActionError, refetch, act };
}
