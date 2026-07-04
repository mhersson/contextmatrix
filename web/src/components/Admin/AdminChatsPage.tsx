import { useCallback, useEffect, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { ChatSession } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ChatsTable } from './ChatsTable';

function errorMessage(err: unknown, fallback: string): string {
  return isAPIError(err) ? err.error : fallback;
}

/** Admin-only Chats page: metadata + lifecycle management for every chat
 * session on the instance. Owns all data fetching and mutations; the table
 * it renders is purely presentational. Deliberately no navigation into
 * transcripts — the backend has no admin transcript route either. */
export function AdminChatsPage() {
  const [chats, setChats] = useState<ChatSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ChatSession | null>(null);

  const refetch = useCallback(async () => {
    try {
      const list = await api.adminListChats();
      setChats(list);
      setListError(null);
    } catch (err) {
      setListError(errorMessage(err, 'Failed to load chats.'));
    } finally {
      setLoading(false);
    }
  }, []);

  // Mount-only fetch, delegated to refetch (also used after every mutation
  // below) — mirrors AdminCredentialsPage.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refetch();
  }, [refetch]);

  const end = useCallback(
    async (id: string) => {
      setActionError(null);
      try {
        await api.adminEndChat(id);
        await refetch();
      } catch (err) {
        setActionError(errorMessage(err, 'Failed to end chat.'));
      }
    },
    [refetch],
  );

  const remove = useCallback(
    async (id: string) => {
      setActionError(null);
      try {
        await api.adminDeleteChat(id);
        await refetch();
      } catch (err) {
        setActionError(errorMessage(err, 'Failed to delete chat.'));
      }
    },
    [refetch],
  );

  return (
    <div className="p-6 flex flex-col gap-4">
      <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
        Chats
      </h1>

      {actionError && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {actionError}
        </div>
      )}

      <ChatsTable
        chats={chats}
        loading={loading}
        error={listError}
        onEnd={(c) => void end(c.id)}
        onDelete={(c) => setPendingDelete(c)}
      />

      <ConfirmModal
        open={pendingDelete !== null}
        title="Delete chat?"
        message={`Deleting "${pendingDelete?.title || pendingDelete?.id}" permanently removes its transcript. Cost totals remain in the dashboard aggregates.`}
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => {
          const c = pendingDelete;
          setPendingDelete(null);
          if (c) void remove(c.id);
        }}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
