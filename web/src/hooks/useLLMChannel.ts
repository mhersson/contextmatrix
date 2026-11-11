import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import { errorMessage } from '../lib/errors';
import type { LLMChannel, LLMChannelView } from '../types';
import { useSSEBus } from './useSSEBus';

const POLL_MS = 60_000;
const EVENT_DEBOUNCE_MS = 1_000;

export interface LLMChannelState {
  view: LLMChannelView | null;
  setMode: (mode: LLMChannel) => Promise<void>;
  /** The last failed switch, cleared by the next attempt. */
  error: string | null;
}

/**
 * The global LLM channel. Fetches on mount and reconnect; while Claude Code
 * is configured (or before the first answer) it also refetches on
 * llm_channel and worker events and every minute.
 */
export function useLLMChannel(): LLMChannelState {
  const [view, setView] = useState<LLMChannelView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { subscribe, reconnectEpoch } = useSSEBus();
  // Bumped by every fetch and switch so a slower, older answer never
  // overwrites a newer one.
  const seqRef = useRef(0);

  const refresh = useCallback(() => {
    const seq = ++seqRef.current;
    api
      .getLLMChannel()
      .then((v) => {
        if (seq === seqRef.current) setView(v);
      })
      .catch(() => {
        // Keep the last view; the next poll or event retries.
      });
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh, reconnectEpoch]);

  // The token is config-only, so a server without one stays without one
  // until a restart, which reconnects SSE and refetches above.
  const live = view === null || view.claude_code;

  useEffect(() => {
    if (!live) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const debounced = () => {
      clearTimeout(timer);
      timer = setTimeout(refresh, EVENT_DEBOUNCE_MS);
    };
    const offChannel = subscribe('llm_channel.*', debounced);
    const offWorker = subscribe('worker.*', debounced);
    const poll = setInterval(refresh, POLL_MS);
    return () => {
      offChannel();
      offWorker();
      clearTimeout(timer);
      clearInterval(poll);
    };
  }, [live, subscribe, refresh]);

  const setMode = useCallback(async (mode: LLMChannel) => {
    const seq = ++seqRef.current;
    setError(null);
    try {
      const v = await api.putLLMChannel(mode);
      if (seq === seqRef.current) setView(v);
    } catch (err) {
      setError(errorMessage(err, 'Could not switch the LLM channel'));
    }
  }, []);

  return { view, setMode, error };
}
