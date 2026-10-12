import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { JiraStatus } from '../types';

/**
 * Jira configuration for the new-playbook form (GET /api/jira/status),
 * fetched each time `enabled` turns on so a fixed token shows up without a
 * reload. Null until it answers, and on failure: the import button stays hidden.
 */
export function useJiraStatus(enabled: boolean): JiraStatus | null {
  const [status, setStatus] = useState<JiraStatus | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    api
      .getJiraStatus()
      .then((s) => {
        if (!cancelled) setStatus(s);
      })
      .catch(() => {
        /* hidden on failure by design */
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  return status;
}
