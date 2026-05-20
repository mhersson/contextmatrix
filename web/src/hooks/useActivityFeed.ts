import { useEffect, useState } from 'react';
import type { BoardEvent } from '../types';
import type { ActivityEntry } from '../components/Board/NowRail';
import { api } from '../api/client';
import { useSSEBus } from './useSSEBus';

export interface ActivityFeedState {
  entries: ActivityEntry[];
  backfillLoaded: boolean;
}

export function useActivityFeed(project: string | null | undefined): ActivityFeedState {
  const [entries, setEntries] = useState<ActivityEntry[]>([]);
  const [backfillLoaded, setBackfillLoaded] = useState(false);
  const bus = useSSEBus();

  // In-render reset on project change. This pattern (a `prev*` state marker
  // compared in render) replaces a `useEffect(..., [project])` that called
  // setState — the effect path was flagged by react-hooks/set-state-in-effect
  // because it produced a cascading render after the project switch.
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setEntries([]);
    setBackfillLoaded(false);
  }

  // Subscribe to SSE for the NowRail activity feed.
  // Action vocabulary is normalised to the same set the backfill emits
  // (the raw activity_log `Action` field — `"claimed"`, `"state_changed"`,
  // `"released"`) so the dedup-by-id below is symmetric across both channels.
  // The event id includes the action so the same card_id+timestamp+agent
  // tuple from two different SSE events doesn't collide.
  //
  // `agent` defaults to "system" when missing — matches the activity log
  // convention used by appendStateChangeLog for stall/parent auto-transitions
  // and other server-side state changes. Without this, system-driven SSE
  // events would silently never reach NowRail.
  useEffect(() => {
    if (!project) return;
    const handler = (evt: BoardEvent) => {
      if (evt.project !== project) return;
      const action =
        evt.type === 'card.claimed' ? 'claimed' :
        evt.type === 'card.state_changed' ? 'state_changed' :
        evt.type === 'card.released' ? 'released' :
        null;
      if (!action) return;
      const agent = evt.agent || 'system';
      setEntries((curr) => [
        { id: `${evt.timestamp}-${evt.card_id}-${agent}-${action}`, agent, action, cardId: evt.card_id, ts: evt.timestamp },
        ...curr,
      ].slice(0, 50));
    };
    const unsubs = [
      bus.subscribe('card.claimed', handler),
      bus.subscribe('card.state_changed', handler),
      bus.subscribe('card.released', handler),
    ];
    return () => { unsubs.forEach((u) => u()); };
  }, [bus, project]);

  // One-shot historical activity backfill on mount / project change. SSE
  // handles forward updates; this fills in entries older than the page load.
  // Backfill ids use the same shape as the SSE branch above so the merge
  // dedup is symmetric — an event delivered by both channels collapses to
  // a single entry.
  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    api.getActivity(project, 50)
      .then((resp) => {
        if (cancelled) return;
        const backfill: ActivityEntry[] = resp.items.map((e) => ({
          id: `${e.ts}-${e.card_id}-${e.agent}-${e.action}`,
          agent: e.agent,
          action: e.action,
          cardId: e.card_id,
          ts: e.ts,
        }));
        // Merge with any live entries that arrived before backfill resolved,
        // dedup by id, sort newest-first.
        setEntries((curr) => {
          const seen = new Set<string>();
          const merged = [...curr, ...backfill].filter((e) => {
            if (seen.has(e.id)) return false;
            seen.add(e.id);
            return true;
          });
          merged.sort((a, b) => b.ts.localeCompare(a.ts));
          return merged.slice(0, 50);
        });
        setBackfillLoaded(true);
      })
      .catch(() => {
        // Non-fatal: SSE still populates going forward.
      });
    return () => { cancelled = true; };
  }, [project]);

  return { entries, backfillLoaded };
}
