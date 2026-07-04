import { useCallback, useEffect, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { AdminUser, InviteInfo } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { CreateUserModal, type CreateUserInput } from './CreateUserModal';
import { InviteLinkDialog } from './InviteLinkDialog';
import { UsersTable } from './UsersTable';

// Destructive direction of each row toggle — the other direction (promote,
// enable) runs immediately without confirmation.
type ConfirmKind = 'demote' | 'disable';
interface PendingConfirm {
  username: string;
  kind: ConfirmKind;
}

function errorMessage(err: unknown, fallback: string): string {
  return isAPIError(err) ? err.error : fallback;
}

/** Admin-only Users page: list, create (with invite link), and per-row
 * role/status management. Owns all data fetching and mutations; the
 * modals and table row it renders are purely presentational. */
export function AdminUsersPage() {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  const [invite, setInvite] = useState<InviteInfo | null>(null);
  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirm | null>(null);

  const refetch = useCallback(async () => {
    try {
      const list = await api.adminListUsers();
      setUsers(list);
      setListError(null);
    } catch (err) {
      setListError(errorMessage(err, 'Failed to load users.'));
    } finally {
      setLoading(false);
    }
  }, []);

  // Mount-only fetch, delegated to refetch (also used after every mutation
  // below) rather than duplicated inline — mirrors useBoard.ts's fetchData
  // effect. setState-in-effect is intentional: this effect's whole purpose
  // is to trigger the initial load.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refetch();
  }, [refetch]);

  const patch = useCallback(
    async (username: string, body: { is_admin?: boolean; disabled?: boolean }) => {
      setActionError(null);
      try {
        await api.adminPatchUser(username, body);
        await refetch();
      } catch (err) {
        setActionError(errorMessage(err, 'Action failed.'));
      }
    },
    [refetch],
  );

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
