import { useEffect, useState } from 'react';

// Module-level cache + inflight dedup: CardPanel and CreateCardPanel can be
// mounted simultaneously and every re-mount would otherwise refetch the
// ~400KB catalog. A success is cached for the page lifetime; failures are
// NOT cached (inflight clears in finally), so a later mount retries.
//
// The cache holds rich entries ({ id, contextLength }); the derived slug list
// and context-length map are memoized once each so cache hits return stable
// references (React bails out of the re-render).
interface ORModel {
  id: string;
  contextLength: number;
}

let cache: ORModel[] | null = null;
let inflight: Promise<ORModel[]> | null = null;
let slugCache: string[] | null = null;
let ctxCache: Record<string, number> | null = null;

function getCatalog(): Promise<ORModel[]> {
  if (cache) return Promise.resolve(cache);
  inflight ??= fetch('https://openrouter.ai/api/v1/models')
    .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`${r.status}`))))
    .then((body: { data?: { id?: string; context_length?: number }[] }) => {
      const models = (body.data ?? [])
        .map((m) => ({ id: m.id ?? '', contextLength: m.context_length ?? 0 }))
        .filter((m) => m.id)
        .sort((a, b) => a.id.localeCompare(b.id));
      cache = models;
      return models;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

// slugsFrom / ctxFrom derive the two view shapes once and memoize them at module
// scope. `cache` is written once and never mutated, so the memo stays valid.
function slugsFrom(models: ORModel[]): string[] {
  slugCache ??= models.map((m) => m.id);
  return slugCache;
}

function ctxFrom(models: ORModel[]): Record<string, number> {
  if (!ctxCache) {
    const map: Record<string, number> = {};
    for (const m of models) {
      if (m.contextLength > 0) map[m.id] = m.contextLength;
    }
    ctxCache = map;
  }
  return ctxCache;
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
  const [slugs, setSlugs] = useState<string[]>(() => (cache ? slugsFrom(cache) : []));
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    getCatalog()
      .then((models) => {
        // Same array reference as the seeded state on a cache hit, so React
        // bails out of the re-render.
        if (!cancelled) setSlugs(slugsFrom(models));
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

/**
 * OpenRouter context-window sizes keyed by model slug, from the same cached
 * catalog fetch. Used by the chat header to show the context-usage % when the
 * dedicated chat backend serves chat (OpenRouter slugs, no config allowlist).
 * On any failure the hook returns {} — the % indicator then hides.
 */
export function useOpenRouterContextLengths(enabled: boolean): Record<string, number> {
  const [lengths, setLengths] = useState<Record<string, number>>(() =>
    cache ? ctxFrom(cache) : {},
  );
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    getCatalog()
      .then((models) => {
        if (!cancelled) setLengths(ctxFrom(models));
      })
      .catch(() => {
        /* indicator-hidden fallback by design */
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return lengths;
}
