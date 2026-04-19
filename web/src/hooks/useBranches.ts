import { useEffect, useState } from 'react';
import { api } from '../api/client';

export interface BranchesResult {
  branches: string[];
  loading: boolean;
  error: boolean;
}

export function useBranches(project: string, enabled: boolean): BranchesResult {
  const [state, setState] = useState<BranchesResult>(() => ({
    branches: [],
    loading: enabled,
    error: false,
  }));
  const [prevProject, setPrevProject] = useState(project);
  const [prevEnabled, setPrevEnabled] = useState(enabled);

  if (project !== prevProject || enabled !== prevEnabled) {
    setPrevProject(project);
    setPrevEnabled(enabled);
    setState({ branches: [], loading: enabled, error: false });
  }

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    api.fetchBranches(project)
      .then((data) => {
        if (!cancelled) setState({ branches: data, loading: false, error: false });
      })
      .catch(() => {
        if (!cancelled) setState((prev) => ({ ...prev, loading: false, error: true }));
      });
    return () => {
      cancelled = true;
    };
  }, [project, enabled]);

  return state;
}
