import { useEffect, useState } from 'react';
import { api } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import type {
  BoardEvent,
  JiraImportDoneEvent,
  JiraImportProgressEvent,
  JiraImportResult,
  JiraImportState,
} from '../../types';

// The server bus drops events for a slow subscriber; this is the backstop.
const CATCH_UP_MS = 5_000;

export interface ImportRun {
  status: JiraImportState['status'];
  results: JiraImportResult[];
  total: number;
  playbookId?: string;
  error?: string;
}

const started = (total: number): ImportRun => ({ status: 'running', results: [], total });

function fromState(s: JiraImportState): ImportRun {
  return { status: s.status, results: s.results, total: s.total, playbookId: s.playbook_id, error: s.error };
}

// Server state replaces what the events built once it knows at least as much.
function merge(current: ImportRun, next: ImportRun): ImportRun {
  if (current.status !== 'running') return current;
  if (next.status !== 'running' || next.results.length >= current.results.length) return next;
  return current;
}

function progressResult(d: JiraImportProgressEvent): JiraImportResult {
  return {
    key: d.key,
    status: d.status,
    project: d.project || undefined,
    card: d.card_id || undefined,
    reason: d.reason || undefined,
  };
}

/**
 * Follows one background Jira import: progress and completion from the SSE
 * bus, caught up from GET /api/jira/imports/{id} when it starts, on every
 * reconnect, and on a slow timer while it runs.
 */
export function useJiraImportRun(importId: string | null, total: number): ImportRun | null {
  const { subscribe, reconnectEpoch } = useSSEBus();
  const [run, setRun] = useState<ImportRun | null>(null);
  const running = !run || run.status === 'running';

  useEffect(() => {
    if (!importId) return;
    const mine = (e: BoardEvent) => e.data?.import_id === importId;
    const offProgress = subscribe('jira_import.progress', (e) => {
      if (!mine(e)) return;
      const r = progressResult(e.data as unknown as JiraImportProgressEvent);
      setRun((cur) => {
        const base = cur ?? started(total);
        if (base.status !== 'running') return base;
        return { ...base, results: [...base.results.filter((x) => x.key !== r.key), r] };
      });
    });
    const offDone = subscribe('jira_import.done', (e) => {
      if (!mine(e)) return;
      const d = e.data as unknown as JiraImportDoneEvent;
      setRun((cur) => ({
        ...(cur ?? started(total)),
        status: d.status,
        results: d.results ?? [],
        playbookId: d.playbook_id || undefined,
        error: d.error || undefined,
      }));
    });
    return () => {
      offProgress();
      offDone();
    };
  }, [importId, subscribe, total]);

  useEffect(() => {
    if (!importId || !running) return;
    let cancelled = false;
    const catchUp = () => {
      api
        .getJiraImport(importId)
        .then((s) => {
          if (!cancelled) setRun((cur) => merge(cur ?? started(total), fromState(s)));
        })
        .catch(() => {
          // The next event or tick retries.
        });
    };
    catchUp();
    const timer = window.setInterval(catchUp, CATCH_UP_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [importId, running, reconnectEpoch, total]);

  return run ?? (importId ? started(total) : null);
}
