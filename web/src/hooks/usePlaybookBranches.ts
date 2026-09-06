import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { BranchesResult } from './useBranches';

// A playbook's base branch applies to every repository it spans, so the
// dropdown may only offer branches that exist in all of them: the sorted
// intersection of each project's branch list. One failing project is an
// error for the whole list rather than a silently shorter one.
export function usePlaybookBranches(projects: string[], enabled: boolean): BranchesResult {
  const key = projects.join('\u0000');
  const active = enabled && projects.length > 0;
  const [state, setState] = useState<BranchesResult>({ branches: [], loading: active, error: false });
  const [prevKey, setPrevKey] = useState(key);
  const [prevActive, setPrevActive] = useState(active);

  if (key !== prevKey || active !== prevActive) {
    setPrevKey(key);
    setPrevActive(active);
    setState({ branches: [], loading: active, error: false });
  }

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    Promise.all(projects.map((p) => api.fetchBranches(p)))
      .then((lists) => {
        if (cancelled) return;
        const common = lists.reduce<string[]>((acc, list) => acc.filter((b) => list.includes(b)), lists[0] ?? []);
        setState({ branches: [...new Set(common)].sort(), loading: false, error: false });
      })
      .catch(() => {
        if (!cancelled) setState((prev) => ({ ...prev, loading: false, error: true }));
      });
    return () => { cancelled = true; };
    // key stands in for the projects array identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, active]);

  return state;
}
