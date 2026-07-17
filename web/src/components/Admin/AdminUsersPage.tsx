import { useState } from 'react';
import { api } from '../../api/client';
import { useAdminResource } from '../../hooks/useAdminResource';
import { errorMessage } from '../../lib/errors';
import type { InviteInfo } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { CreateUserModal, type CreateUserInput } from './CreateUserModal';
import { InviteLinkDialog } from './InviteLinkDialog';
import { UsersTable } from './UsersTable';

// Destructive direction of each row toggle - the other direction (promote,
// enable) runs immediately without confirmation.
type ConfirmKind = 'demote' | 'disable';
interface PendingConfirm {
  username: string;
  kind: ConfirmKind;
}

const fetchUsers = () => api.adminListUsers();

/** Admin-only Users page: list, create (with invite link), and per-row
 * role/status management. Owns all data fetching and mutations; the
 * modals and table row it renders are purely presentational. */
export function AdminUsersPage() {
  const {
    items: users,
    loading,
    listError,
    actionError,
    setActionError,
    refetch,
    act,
  } = useAdminResource(fetchUsers, [], 'Failed to load users.');

  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  const [invite, setInvite] = useState<InviteInfo | null>(null);
  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirm | null>(null);

  const patch = (username: string, body: { is_admin?: boolean; disabled?: boolean }) =>
    act(() => api.adminPatchUser(username, body), 'Action failed.');

  const createUser = async (input: CreateUserInput) => {
    setCreating(true);
    setCreateError(null);
    try {
      const { invite: newInvite } = await api.adminCreateUser(input);
      setCreateOpen(false);
      await refetch();
      setInvite(newInvite);
    } catch (err) {
      setCreateError(errorMessage(err, 'Failed to create user.'));
    } finally {
      setCreating(false);
    }
  };

  // Deliberately no refetch - regenerating a link changes no listed field.
  const regenerateLink = async (username: string) => {
    setActionError(null);
    try {
      setInvite(await api.adminRegenerateLink(username));
    } catch (err) {
      setActionError(errorMessage(err, 'Failed to generate link.'));
    }
  };

  const confirmPending = async () => {
    if (!pendingConfirm) return;
    const { username, kind } = pendingConfirm;
    setPendingConfirm(null);
    await patch(username, kind === 'disable' ? { disabled: true } : { is_admin: false });
  };

  return (
    <div className="p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
          Users
        </h1>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="rounded py-1.5 px-4 font-medium"
          style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)' }}
        >
          New user
        </button>
      </div>

      {actionError && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {actionError}
        </div>
      )}

      <UsersTable
        users={users}
        loading={loading}
        error={listError}
        onToggleAdmin={(u) =>
          u.is_admin
            ? setPendingConfirm({ username: u.username, kind: 'demote' })
            : void patch(u.username, { is_admin: true })
        }
        onToggleDisabled={(u) =>
          u.disabled
            ? void patch(u.username, { disabled: false })
            : setPendingConfirm({ username: u.username, kind: 'disable' })
        }
        onRegenerateLink={(username) => void regenerateLink(username)}
      />

      <CreateUserModal
        open={createOpen}
        busy={creating}
        error={createError}
        onClose={() => setCreateOpen(false)}
        onCreate={(input) => void createUser(input)}
      />

      <InviteLinkDialog
        open={invite !== null}
        token={invite?.token ?? ''}
        purpose={invite?.purpose ?? 'invite'}
        onClose={() => setInvite(null)}
      />

      <ConfirmModal
        open={pendingConfirm !== null}
        title={pendingConfirm?.kind === 'disable' ? 'Disable user?' : 'Remove admin rights?'}
        message={
          pendingConfirm?.kind === 'disable'
            ? `${pendingConfirm.username} will not be able to log in until re-enabled.`
            : `${pendingConfirm?.username} will lose admin access.`
        }
        variant="danger"
        confirmLabel={pendingConfirm?.kind === 'disable' ? 'Disable' : 'Remove admin'}
        onConfirm={() => void confirmPending()}
        onCancel={() => setPendingConfirm(null)}
      />
    </div>
  );
}
