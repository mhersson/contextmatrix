import { useEffect, useState } from 'react';

// Module-level cache + inflight dedup: CardPanel and CreateCardPanel can be
// mounted simultaneously and every re-mount would otherwise refetch the
// ~400KB catalog. A success is cached for the page lifetime; failures are
// NOT cached (inflight clears in finally), so a later mount retries.
let cache: string[] | null = null;
let inflight: Promise<string[]> | null = null;

function getCatalog(): Promise<string[]> {
  // Cache may have been populated by another mount after this component
  // seeded its initial state (e.g. when `enabled` flips later); resolve it
  // through the same promise path so consumers have a single code shape.
  if (cache) return Promise.resolve(cache);
  inflight ??= fetch('https://openrouter.ai/api/v1/models')
    .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`${r.status}`))))
    .then((body: { data?: { id?: string }[] }) => {
      const ids = (body.data ?? [])
        .map((m) => m.id ?? '')
        .filter(Boolean)
        .sort();
      cache = ids;
      return ids;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/**
 * Model slugs from OpenRouter's public catalog for pin autocomplete.
 * Endpoint is CORS-open and unauthenticated. On any failure the hook
 * returns [] — the pin inputs then behave as plain free text.
 *
 * Concurrent mounts join the same inflight request; later mounts read the
 * module-level cache without refetching.
 */
export function useOpenRouterModels(enabled: boolean): string[] {
  const [slugs, setSlugs] = useState<string[]>(() => cache ?? []);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    getCatalog()
      .then((ids) => {
        // Same array reference as the seeded state on a cache hit, so React
        // bails out of the re-render.
        if (!cancelled) setSlugs(ids);
      })
      .catch(() => {
        /* free-text fallback by design */
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return slugs;
}
