import { useCallback, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { api, describeApiError, isAPIError } from '../../api/client';
import { notifyChatSessionsChanged } from '../../hooks/useChatSessions';

export type CreateRequest = Parameters<typeof api.createChat>[0];

const ARCHITECT_DOWN = 'The architect is not reachable. Check that its service is running, then try again.';
const NO_ARCHITECT = 'This server has no architect configured. Ask an admin to add one, or create a chat.';
export const NEED_NOT_POSTED = 'The blueprint started, but the need was not posted. Create again to post it.';

/** What the dialog says when CM refuses a blueprint from a hand-off, by error code. */
export const HANDOFF_REFUSALS: Readonly<Record<'HANDOFF_TAKEN' | 'HANDOFF_REPLACED', string>> = {
  HANDOFF_TAKEN: 'A blueprint was already started from this hand-off.',
  HANDOFF_REPLACED: 'A newer hand-off replaced this one. Review the newer one in the chat.',
};

function createFailure(e: unknown, blueprint: boolean): string {
  if (isAPIError(e)) {
    if (blueprint && e.code === 'CHAT_KIND_UNAVAILABLE') return NO_ARCHITECT;
    if (e.code === 'HANDOFF_TAKEN' || e.code === 'HANDOFF_REPLACED') return HANDOFF_REFUSALS[e.code];
  }
  return describeApiError(e, 'Could not create the session. Try again.');
}

/**
 * Creates a session, opens it, posts a handed-over need when given one and
 * navigates to it. A blueprint whose architect does not start, or whose need
 * is not posted, keeps the dialog open with the error; creating again with the
 * same request, or from the same hand-off, reopens that session (and resends
 * the need, which CM posts at most once) instead of making another.
 */
export function useCreateSession(onDone: () => void) {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pendingRef = useRef<{ request: string; id: string } | null>(null);

  const create = useCallback(
    async (req: CreateRequest, need?: string) => {
      const blueprint = req.kind === 'architect';
      // A hand-off makes at most one blueprint, so its retry never depends on settings changed since.
      const request = req.origin_chat ? `origin:${req.origin_chat}#${req.origin_seq}` : JSON.stringify(req);
      setCreating(true);
      setError(null);
      try {
        let id = pendingRef.current?.request === request ? pendingRef.current.id : null;
        if (id === null) {
          id = (await api.createChat(req)).id;
          // The sidebar lists the new row before the worker is warm.
          notifyChatSessionsChanged();
        }
        try {
          await api.openChat(id);
          pendingRef.current = null;
        } catch (openErr) {
          if (blueprint) {
            pendingRef.current = { request, id };
            setError(isAPIError(openErr) && openErr.code === 'CHAT_KIND_UNAVAILABLE' ? NO_ARCHITECT : ARCHITECT_DOWN);
            return;
          }
          // A chat row exists; the cold pane's Reopen retries.
          console.warn('openChat failed; user can Reopen manually', openErr);
        }
        if (need !== undefined) {
          try {
            await api.sendChatMessage(id, need, { handoff_need: true });
          } catch (sendErr) {
            console.warn('posting the handed-over need failed; Create retries it', sendErr);
            pendingRef.current = { request, id };
            // The open landed, so the listed status is stale.
            notifyChatSessionsChanged();
            setError(NEED_NOT_POSTED);
            return;
          }
        }
        notifyChatSessionsChanged();
        onDone();
        navigate(`/chat/${id}`);
      } catch (e) {
        setError(createFailure(e, blueprint));
      } finally {
        setCreating(false);
      }
    },
    [navigate, onDone],
  );

  const clearError = useCallback(() => setError(null), []);

  return { create, creating, error, clearError };
}
