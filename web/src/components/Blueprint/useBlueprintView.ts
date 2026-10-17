import { useCallback, useEffect, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { BlueprintLiveFrame, BlueprintView } from '../../types';
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

/** The architect's view of a blueprint session: one GET, then the live frames on its chat stream. */
export function useBlueprintView(chatId: string, frames: FrameSource): { state: BlueprintState; retry: () => void } {
  const [state, setState] = useState<BlueprintState>({ status: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [prevChatId, setPrevChatId] = useState(chatId);
  if (chatId !== prevChatId) {
    setPrevChatId(chatId);
    setState({ status: 'loading' });
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

  useEffect(() => {
    let alive = true;
    api.getBlueprintView(chatId).then(
      (view) => {
        if (alive) setState((s) => withFetchedView(s, normalizeView(view)));
      },
      (err: unknown) => {
        if (!alive) return;
        const status = isAPIError(err) && err.code && ABSENT_CODES.has(err.code) ? 'absent' : 'unreachable';
        // A snapshot that landed first is a whole view; the failed fetch changes nothing.
        setState((s) => (s.status === 'ready' ? s : { status }));
      },
    );
    return () => {
      alive = false;
    };
  }, [chatId, attempt]);

  const retry = useCallback(() => {
    setState({ status: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  return { state, retry };
}
