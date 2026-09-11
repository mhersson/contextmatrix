import { useEffect, useRef, useState } from 'react';
import { api } from '../../api/client';
import { errorMessage } from '../../lib/errors';
import type { SelectorLadders, SelectorPreview } from '../../types';
import { ladderKey } from './ladder';

/** A drag emits a step every few milliseconds; one request per pause is enough. */
export const PREVIEW_DEBOUNCE_MS = 100;

interface PreviewState {
  preview: SelectorPreview | null;
  /** Key of the ladders the preview (or the error) answers for. */
  forKey: string | null;
  error: string | null;
}

export interface SelectorPreviewResult {
  preview: SelectorPreview | null;
  pending: boolean;
  error: string | null;
}

/**
 * Debounced server preview for the ladders on screen. Pending is derived -
 * the shown ladders differ from the ones the last answer was for - so a drag
 * step never sets state synchronously. An error keeps the last good preview
 * and is cleared by the next successful answer. A superseded request is
 * aborted and its late answer ignored.
 */
export function useSelectorPreview(ladders: SelectorLadders, enabled: boolean): SelectorPreviewResult {
  const [state, setState] = useState<PreviewState>({ preview: null, forKey: null, error: null });
  const key = ladderKey(ladders);

  // The request reads the ladders through a ref so the effect keys on the
  // value (`key`), not the object: a refetch that yields equal values must
  // not fire another preview.
  const laddersRef = useRef(ladders);
  useEffect(() => {
    laddersRef.current = ladders;
  }, [ladders]);

  const latestKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    latestKeyRef.current = key;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      api
        .adminSelectorPreview(laddersRef.current, controller.signal)
        .then((preview) => {
          if (latestKeyRef.current !== key) return;
          setState({ preview, forKey: key, error: null });
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
  }, [key, enabled]);

  return { preview: state.preview, pending: enabled && state.forKey !== key, error: state.error };
}
