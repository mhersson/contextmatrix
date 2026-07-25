import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { UserSummary } from '../types';

const EMPTY: UserSummary[] = [];

// Module-level cache + inflight dedup: card panels and the create panel can
// mount simultaneously; a success is cached for the page lifetime, failures
// are not (a later mount retries).
let cache: UserSummary[] | null = null;
let inflight: Promise<UserSummary[]> | null = null;

function getUsers(): Promise<UserSummary[]> {
  if (cache) return Promise.resolve(cache);
  inflight ??= api
    .listUsers()
    .then((resp) => {
      cache = resp;
      return resp;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/**
 * User roster for assignee pickers (GET /api/users) - session-gated, multi
 * mode only. On failure returns an empty roster; the roster is a
 * convenience, so a failed fetch must not break the panel.
 */
export function useUsers(enabled: boolean): UserSummary[] {
  const [result, setResult] = useState<UserSummary[]>(cache ?? EMPTY);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    getUsers()
      .then((r) => {
        if (!cancelled) setResult(r);
      })
      .catch(() => {
        /* empty-roster fallback by design */
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return result;
}
