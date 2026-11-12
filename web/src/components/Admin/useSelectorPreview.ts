import { useEffect, useRef, useState } from 'react';
import { api } from '../../api/client';
import { errorMessage } from '../../lib/errors';
import type { SelectorCatalog, SelectorLadders, SelectorPreview } from '../../types';
import { ladderKey } from './ladder';

/** A drag emits a step every few milliseconds; one request per pause is enough. */
export const PREVIEW_DEBOUNCE_MS = 100;

interface PreviewState {
  preview: SelectorPreview | null;
  /** Key of the ladders the preview (or the error) answers for. */
  forKey: string | null;
  /** Headroom the shown preview was computed with; null until one lands. */
  forHeadroom: number | null;
  error: string | null;
}

export interface SelectorPreviewResult {
  preview: SelectorPreview | null;
  pending: boolean;
  error: string | null;
  /**
   * Headroom the shown preview was computed with, null before the first
   * answer. While the field on screen is invalid no request fires, so this
   * is what the panel may truthfully claim.
   */
  appliedHeadroom: number | null;
}

/**
 * Debounced server preview for the ladders and headroom on screen. Pending is
 * derived - the shown ladders differ from the ones the last answer was for -
 * so a drag step never sets state synchronously. An error keeps the last good
 * preview and is cleared by the next successful answer. A superseded request
 * is aborted and its late answer ignored. The server applies its own
 * blacklist; `blacklist` is the catalog's copy of it, so a change there
 * (after an add or a delist) re-runs the preview. `catalog` is the catalog
 * on screen: an answer from another one stays pending, calls
 * `onCatalogMismatch` and asks again once that settles; a new catalog
 * re-runs the preview.
 */
export function useSelectorPreview(
  ladders: SelectorLadders,
  headroom: number,
  enabled: boolean,
  blacklist: readonly string[] = [],
  catalog: SelectorCatalog = 'endpoint',
  onCatalogMismatch?: () => Promise<unknown> | void,
): SelectorPreviewResult {
  const [state, setState] = useState<PreviewState>({ preview: null, forKey: null, forHeadroom: null, error: null });
  // Bumped after a mismatch's catalog refresh, which may leave the key as it was.
  const [retry, setRetry] = useState(0);
  const key = `${ladderKey(ladders)}|${headroom}|${blacklist.join(',')}|${catalog}`;

  // The request reads the ladders through a ref so the effect keys on the
  // value (`key`), not the object: a refetch that yields equal values must
  // not fire another preview.
  const laddersRef = useRef(ladders);
  useEffect(() => {
    laddersRef.current = ladders;
  }, [ladders]);

  const headroomRef = useRef(headroom);
  useEffect(() => {
    headroomRef.current = headroom;
  }, [headroom]);

  const catalogRef = useRef(catalog);
  const mismatchRef = useRef(onCatalogMismatch);
  useEffect(() => {
    catalogRef.current = catalog;
    mismatchRef.current = onCatalogMismatch;
  }, [catalog, onCatalogMismatch]);

  const latestKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    latestKeyRef.current = key;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      api
        .adminSelectorPreview(laddersRef.current, headroomRef.current, controller.signal)
        .then((preview) => {
          if (latestKeyRef.current !== key) return;
          if (preview.catalog !== catalogRef.current) {
            void Promise.resolve(mismatchRef.current?.()).finally(() => setRetry((n) => n + 1));
            return;
          }
          setState({ preview, forKey: key, forHeadroom: headroomRef.current, error: null });
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted || latestKeyRef.current !== key) return;
          setState((s) => ({ ...s, forKey: key, error: errorMessage(err, 'Preview failed.') }));
        });
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [key, enabled, retry]);

  return { preview: state.preview, pending: enabled && state.forKey !== key, error: state.error, appliedHeadroom: state.forHeadroom };
}
