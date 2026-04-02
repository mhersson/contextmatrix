import { useState, useEffect, useCallback, useRef } from 'react';
import type { DashboardData, BoardEvent } from '../types';
import { api } from '../api/client';
import { useSSE } from './useSSE';

const REFRESH_INTERVAL = 30000;

export function useProjectSummaries(projectNames: string[]) {
  const [summaries, setSummaries] = useState<Map<string, DashboardData>>(new Map());
  const [loading, setLoading] = useState(true);
  const projectNamesRef = useRef(projectNames);
  projectNamesRef.current = projectNames;

  const fetchAll = useCallback(async () => {
    const names = projectNamesRef.current;
    if (names.length === 0) {
      setSummaries(new Map());
      setLoading(false);
      return;
    }

    const results = await Promise.allSettled(
      names.map(async (name) => {
        const data = await api.getDashboard(name);
        return [name, data] as const;
      })
    );

    const map = new Map<string, DashboardData>();
    for (const result of results) {
      if (result.status === 'fulfilled') {
        map.set(result.value[0], result.value[1]);
      }
    }
    setSummaries(map);
    setLoading(false);
  }, []);

  const projectKey = projectNames.join(',');
  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [fetchAll, projectKey]);

  const handleEvent = useCallback(
    (event: BoardEvent) => {
      if (event.type.startsWith('card.')) {
        fetchAll();
      }
    },
    [fetchAll]
  );

  useSSE({ onEvent: handleEvent });

  return { summaries, loading };
}
