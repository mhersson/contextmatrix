import { useState, useEffect, useRef, useCallback } from 'react';
import type { LogEntry } from '../types';
import { useRingBuffer } from './useRingBuffer';

interface UseRunnerLogsOptions {
  project: string;
  enabled: boolean;
  maxEntries?: number;
  /** When set, the hook connects to the card-scoped session endpoint and
   *  receives only events for this card (server-filtered). */
  cardId?: string;
}

interface UseRunnerLogsResult {
  logs: readonly LogEntry[];
  connected: boolean;
  error: string | null;
  clear: () => void;
}

const MAX_RECONNECT_DELAY = 30000;
const INITIAL_RECONNECT_DELAY = 1000;

/** Build a gap marker LogEntry with the given message. */
function makeGapMarker(message: string): LogEntry {
  return {
    ts: new Date().toISOString(),
    card_id: '',
    type: 'gap',
    content: message,
  };
}

export function useRunnerLogs({
  project,
  enabled,
  maxEntries = 5000,
  cardId,
}: UseRunnerLogsOptions): UseRunnerLogsResult {
  const ringBuffer = useRingBuffer(maxEntries);
  const { append, clear } = ringBuffer;
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectDelayRef = useRef(INITIAL_RECONNECT_DELAY);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const connectRef = useRef<() => void>(() => {});
  const isMountedRef = useRef(true);
  /** Last seen seq number; null means no message received yet. */
  const lastSeqRef = useRef<number | null>(null);
  /** Set to true when a terminal frame is received — suppresses reconnects. */
  const terminalRef = useRef(false);

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current !== null) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    reconnectDelayRef.current = INITIAL_RECONNECT_DELAY;
    setConnected(false);
  }, []);

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    // Reset per-connection state on a fresh intentional connect.
    lastSeqRef.current = null;
    terminalRef.current = false;

    let url = `/api/runner/logs?project=${encodeURIComponent(project)}`;
    if (cardId) {
      url += `&card_id=${encodeURIComponent(cardId)}`;
    }
    const es = new EventSource(url);
    eventSourceRef.current = es;

    es.onopen = () => {
      setConnected(true);
      setError(null);
      reconnectDelayRef.current = INITIAL_RECONNECT_DELAY;
    };

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as Record<string, unknown>;

        if (data.type === 'error') {
          setError((data.content as string) || 'Unknown error');
          return;
        }

        if (data.type === 'terminal') {
          // Server session ended — stop reconnecting.
          terminalRef.current = true;
          if (reconnectTimeoutRef.current !== null) {
            clearTimeout(reconnectTimeoutRef.current);
            reconnectTimeoutRef.current = null;
          }
          setConnected(false);
          return;
        }

        if (data.type === 'dropped') {
          const count = typeof data.count === 'number' ? data.count : 0;
          const marker = makeGapMarker(
            `log stream dropped ${count} event${count !== 1 ? 's' : ''} (server ring-buffer overflow)`,
          );
          append([marker]);
          return;
        }

        // Normal log entry — check for seq gap before appending.
        const entry = data as unknown as LogEntry;
        const seq = typeof data.seq === 'number' ? (data.seq as number) : null;

        const entriesToAdd: LogEntry[] = [];

        if (seq !== null && lastSeqRef.current !== null && seq > lastSeqRef.current + 1) {
          entriesToAdd.push(
            makeGapMarker(
              `seq gap detected: expected ${lastSeqRef.current + 1}, got ${seq} (${seq - lastSeqRef.current - 1} missing)`,
            ),
          );
        }

        if (seq !== null) {
          lastSeqRef.current = seq;
        }

        entriesToAdd.push(entry);
        append(entriesToAdd);
      } catch {
        console.error('Failed to parse runner log entry:', event.data);
      }
    };

    es.onerror = () => {
      setConnected(false);
      es.close();
      eventSourceRef.current = null;

      // Do not reconnect after a clean terminal frame.
      if (terminalRef.current) {
        return;
      }

      const delay = reconnectDelayRef.current;
      setError((prev) => prev ?? `Disconnected. Reconnecting in ${Math.round(delay / 1000)}s...`);

      reconnectTimeoutRef.current = window.setTimeout(() => {
        if (!isMountedRef.current) return;
        // Guard again — terminal may have arrived while timer was pending.
        if (terminalRef.current) return;
        reconnectDelayRef.current = Math.min(
          reconnectDelayRef.current * 2,
          MAX_RECONNECT_DELAY
        );
        connectRef.current();
      }, delay);
    };
  }, [project, cardId, append]);

  // Keep connectRef in sync with connect
  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  // Track mount lifecycle separately — isMountedRef must only change on
  // true mount/unmount, not on every dependency change.
  useEffect(() => {
    isMountedRef.current = true;
    return () => { isMountedRef.current = false; };
  }, []);

  useEffect(() => {
    if (enabled) {
      connect();
    }

    return () => {
      disconnect();
    };
  }, [enabled, connect, disconnect]);

  return { logs: ringBuffer.logs, connected, error, clear };
}
