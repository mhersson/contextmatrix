import { useCallback, useState } from 'react';
import { safeGetString, safeSetString } from '../utils/safeStorage';

const STORAGE_KEY = 'contextmatrix-sidebar-repo-collapsed';

function read(): Record<string, boolean> {
  try {
    const raw = safeGetString(STORAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    return parsed !== null && typeof parsed === 'object' ? (parsed as Record<string, boolean>) : {};
  } catch {
    return {};
  }
}

/** Collapsed state of the sidebar's per-repo project sections, keyed by repo name. */
export function useRepoSectionsCollapsed(): [(repo: string) => boolean, (repo: string) => void] {
  const [state, setState] = useState<Record<string, boolean>>(read);

  const isCollapsed = useCallback((repo: string) => state[repo] === true, [state]);

  const toggle = useCallback((repo: string) => {
    setState((prev) => {
      const next = { ...prev, [repo]: !prev[repo] };
      safeSetString(STORAGE_KEY, JSON.stringify(next));
      return next;
    });
  }, []);

  return [isCollapsed, toggle];
}
