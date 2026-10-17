import { useCallback, useEffect, useMemo, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { BlueprintLiveFrame, BlueprintView, LogEntry } from '../../types';
import { refreshArtifact } from './artifactCache';
import { applyDraft, holdDraft } from './draft';
import { normalizeView } from './stages';
import type { BlueprintState } from './state';

/** Subscribes to the architect's live frames, each as its JSON text; returns the unsubscribe. */
export type FrameSource = (listener: (frame: string) => void) => () => void;

// No blueprint view to show for these: the pane stays a plain transcript.
const ABSENT_CODES = new Set(['CHAT_NOT_FOUND', 'BLUEPRINT_NOT_FOUND', 'CHAT_KIND_UNAVAILABLE']);

function parseFrame(text: string): BlueprintLiveFrame | null {
  try {
    const frame: unknown = JSON.parse(text);
    return typeof frame === 'object' && frame !== null ? (frame as BlueprintLiveFrame) : null;
  } catch {
    return null;
  }
}

// Snapshots are always applied: they carry the whole view and draft and heal any gap.
function reduceFrame(s: BlueprintState, frame: BlueprintLiveFrame): BlueprintState {
  if (frame.type === 'snapshot' && frame.view) {
    return { status: 'ready', view: normalizeView(frame.view), draft: frame.draft ? holdDraft(frame.draft) : null };
  }
  if (frame.type === 'draft' && frame.draft && s.status === 'ready') {
    const draft = applyDraft(s.draft, frame.draft);
    return draft === s.draft ? s : { ...s, draft };
  }
  return s;
}

function withFetchedView(s: BlueprintState, view: BlueprintView): BlueprintState {
  if (s.status !== 'ready') return { status: 'ready', view, draft: null };
  return view.seq >= s.view.seq ? { ...s, view } : s;
}

// Marker and prompt rows are never dropped on the way, unlike blueprint frames; a new one means the view moved.
function newestViewRow(logs: readonly LogEntry[]): LogEntry | null {
  for (let i = logs.length - 1; i >= 0; i--) {
    if (logs[i].kind === 'marker' || logs[i].kind === 'prompt') return logs[i];
  }
  return null;
}

interface Signals {
  connection: number;
  row: LogEntry | null;
  /** GETs owed after the first. */
  refetches: number;
  /** Stream reopenings after the first open. */
  reconnects: number;
}

function nextSignals(s: Signals, connection: number, row: LogEntry | null): Signals {
  // The first open is covered by the initial GET; a drop to 0 is the stream starting over for another chat.
  const reopened = s.connection > 0 && connection > s.connection;
  const newRow = row !== null && row !== s.row;
  return {
    connection,
    row,
    refetches: s.refetches + (reopened || newRow ? 1 : 0),
    reconnects: s.reconnects + (reopened ? 1 : 0),
  };
}

/**
 * The architect's view of a blueprint session: a GET, then the live frames on its
 * chat stream. Frames can be lost, so the view is fetched again after the stream
 * reopens and when a marker or prompt row arrives; fetched views apply by seq.
 */
export function useBlueprintView(
  chatId: string,
  frames: FrameSource,
  connection: number,
  logs: readonly LogEntry[],
): { state: BlueprintState; retry: () => void } {
  const [state, setState] = useState<BlueprintState>({ status: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const row = useMemo(() => newestViewRow(logs), [logs]);
  const [signals, setSignals] = useState<Signals>({ connection, row, refetches: 0, reconnects: 0 });
  const [prevChatId, setPrevChatId] = useState(chatId);
  if (chatId !== prevChatId) {
    setPrevChatId(chatId);
    setState({ status: 'loading' });
    setSignals({ connection, row, refetches: 0, reconnects: 0 });
  } else if (connection !== signals.connection || row !== signals.row) {
    setSignals(nextSignals(signals, connection, row));
  }

  // Frames sent before this subscribes are gone; the GET covers the view and the next snapshot the draft.
  useEffect(
    () =>
      frames((text) => {
        const frame = parseFrame(text);
        if (frame?.type === 'artifact_stored') {
          if (frame.stage && frame.version) refreshArtifact(chatId, frame.stage, frame.version);
        } else if (frame) {
          setState((s) => reduceFrame(s, frame));
        }
      }),
    [chatId, frames],
  );

  // A reopened stream replays no artifact_stored hints; a no-op unless the board is cached.
  useEffect(() => {
    if (signals.reconnects > 0) refreshArtifact(chatId, 'emission', 1);
  }, [chatId, signals.reconnects]);

  useEffect(() => {
    let alive = true;
    // Normalizing before the handlers sends a 200 without a body to the error path.
    api.getBlueprintView(chatId).then(normalizeView).then(
      (view) => {
        if (alive) setState((s) => withFetchedView(s, view));
      },
      (err: unknown) => {
        if (!alive) return;
        const status = isAPIError(err) && err.code && ABSENT_CODES.has(err.code) ? 'absent' : 'unreachable';
        // A snapshot that landed first is a whole view; a refetch that fails changes neither it nor an absent pane.
        setState((s) => (s.status === 'ready' || (s.status === 'absent' && status === 'unreachable') ? s : { status }));
      },
    );
    return () => {
      alive = false;
    };
  }, [chatId, attempt, signals.refetches]);

  const retry = useCallback(() => {
    setState({ status: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  return { state, retry };
}
