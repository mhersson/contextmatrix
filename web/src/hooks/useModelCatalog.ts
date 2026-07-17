import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { ModelCatalogResponse } from '../types';

const EMPTY: ModelCatalogResponse = { source: 'none', models: [] };

// Module-level cache + inflight dedup: CardPanel and CreateCardPanel can be
// mounted simultaneously; a success is cached for the page lifetime, failures
// are not (a later mount retries).
let cache: ModelCatalogResponse | null = null;
let inflight: Promise<ModelCatalogResponse> | null = null;

function getCatalog(): Promise<ModelCatalogResponse> {
  if (cache) return Promise.resolve(cache);
  inflight ??= api
    .getModelCatalog()
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
 * Model catalog for the card pin pickers (GET /api/models) - CM's
 * vendor-screened OpenRouter list or the endpoint's served list. On failure
 * returns the empty catalog; the pin comboboxes then degrade to free text.
 */
export function useModelCatalog(enabled: boolean): ModelCatalogResponse {
  const [result, setResult] = useState<ModelCatalogResponse>(cache ?? EMPTY);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    getCatalog()
      .then((r) => {
        if (!cancelled) setResult(r);
      })
      .catch(() => {
        /* empty-catalog fallback by design */
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return result;
}
