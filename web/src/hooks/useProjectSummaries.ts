import { useState, useEffect, useCallback, useRef } from 'react';
import type { DashboardData } from '../types';
import { api } from '../api/client';
import { useSSEBus } from './useSSEBus';

const REFRESH_INTERVAL = 30000;

export interface UseProjectSummariesResult {
  summaries: Map<string, DashboardData>;
  /** Project names whose latest dashboard fetch rejected. */
  errors: Set<string>;
  loading: boolean;
  refresh: () => Promise<void>;
}

export function useProjectSummaries(projectNames: string[]): UseProjectSummariesResult {
  const [summaries, setSummaries] = useState<Map<string, DashboardData>>(new Map());
  const [errors, setErrors] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);

  const projectNamesRef = useRef(projectNames);
  useEffect(() => {
    projectNamesRef.current = projectNames;
  });

  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  // Monotonic request id; only the latest request commits its result.
  const reqIdRef = useRef(0);

  const fetchAll = useCallback(async () => {
    const names = projectNamesRef.current;
    const reqId = ++reqIdRef.current;

    if (names.length === 0) {
      setSummaries(new Map());
      setErrors(new Set());
      setLoading(false);
      return;
    }

    const results = await Promise.all(
      names.map(async (name) => {
        try {
          const data = await api.getDashboard(name);
          return { name, ok: true as const, data };
        } catch (err) {
          return { name, ok: false as const, error: err };
        }
      }),
    );

    if (reqId !== reqIdRef.current) return; // stale - newer request already in flight

    const map = new Map<string, DashboardData>();
    const failed = new Set<string>();
    for (const r of results) {
      if (r.ok) {
        map.set(r.name, r.data);
      } else {
        failed.add(r.name);
        console.warn(`getDashboard(${r.name}) failed:`, r.error);
      }
    }
    setSummaries(map);
    setErrors(failed);
    setLoading(false);
  }, []);

  // JSON.stringify avoids the collision where two distinct lists (e.g.
  // ['a,b'] vs ['a','b']) hash to the same comma-joined string.
  const projectKey = JSON.stringify(projectNames);
  useEffect(() => {
    const initialTimeout = setTimeout(fetchAll, 0);
    const interval = setInterval(fetchAll, REFRESH_INTERVAL);
    return () => {
      clearTimeout(initialTimeout);
      clearInterval(interval);
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [fetchAll, projectKey]);

  const { subscribe } = useSSEBus();

  useEffect(() => {
    return subscribe('card.*', () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => fetchAll(), 500);
    });
  }, [subscribe, fetchAll]);

  return { summaries, errors, loading, refresh: fetchAll };
}
