import { useState } from 'react';
import { api } from '../../api/client';
import { useAdminResource } from '../../hooks/useAdminResource';
import type { ChatSession } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ChatsTable } from './ChatsTable';

const fetchChats = () => api.adminListChats();

/** Admin-only Chats page: metadata + lifecycle management for every chat
 * session on the instance. Owns all data fetching and mutations; the table
 * it renders is purely presentational. Deliberately no navigation into
 * transcripts - the backend has no admin transcript route either. */
export function AdminChatsPage() {
  const {
    items: chats,
    loading,
    listError,
    actionError,
    act,
  } = useAdminResource(fetchChats, [], 'Failed to load chats.');

  const [pendingDelete, setPendingDelete] = useState<ChatSession | null>(null);

  const end = (id: string) => act(() => api.adminEndChat(id), 'Failed to end chat.');

  const remove = (id: string) => act(() => api.adminDeleteChat(id), 'Failed to delete chat.');

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
