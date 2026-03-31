import { useState, useCallback } from 'react';

const STORAGE_KEY = 'contextmatrix-last-project';

export function useLastProject(): [string | null, (project: string) => void] {
  const [lastProject, setLastProjectState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY);
  });

  const setLastProject = useCallback((project: string) => {
    localStorage.setItem(STORAGE_KEY, project);
    setLastProjectState(project);
  }, []);

  return [lastProject, setLastProject];
}
