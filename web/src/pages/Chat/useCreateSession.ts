import { useCallback, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { api, describeApiError, isAPIError } from '../../api/client';
import { notifyChatSessionsChanged } from '../../hooks/useChatSessions';

export type CreateRequest = Parameters<typeof api.createChat>[0];

const ARCHITECT_DOWN = 'The architect is not reachable. Check that its service is running, then try again.';
const NO_ARCHITECT = 'This server has no architect configured. Ask an admin to add one, or create a chat.';

/**
 * Creates a session, opens it and navigates to it. A blueprint whose
 * architect does not start keeps the dialog open with the error; creating
 * again with the same request reopens that session instead of making another.
 */
export function useCreateSession(onDone: () => void) {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pendingRef = useRef<{ request: string; id: string } | null>(null);

  const create = useCallback(
    async (req: CreateRequest) => {
      const blueprint = req.kind === 'architect';
      const request = JSON.stringify(req);
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
        notifyChatSessionsChanged();
        onDone();
        navigate(`/chat/${id}`);
      } catch (e) {
        setError(
          blueprint && isAPIError(e) && e.code === 'CHAT_KIND_UNAVAILABLE'
            ? NO_ARCHITECT
            : describeApiError(e, 'Could not create the session. Try again.'),
        );
      } finally {
        setCreating(false);
      }
    },
    [navigate, onDone],
  );

  return { create, creating, error };
}
