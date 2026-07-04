import { useCallback, useEffect, useState } from 'react';
import { api } from '../../api/client';
import { errorMessage } from '../../lib/errors';
import type { CredentialInfo } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { CredentialModal } from './CredentialModal';
import { CredentialsTable } from './CredentialsTable';

type ConfirmKind = 'disable' | 'delete';
interface PendingConfirm {
  name: string;
  kind: ConfirmKind;
}

/** Admin-only Credentials page: list, create/rotate (via CredentialModal),
 * and per-row disable/enable/delete management. Owns all data fetching and
 * mutations; the table and row it renders are purely presentational, and
 * CredentialModal owns its own create/rotate API call (see that file's
 * doc comment for why). */
export function AdminCredentialsPage() {
  const [credentials, setCredentials] = useState<CredentialInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const [modalOpen, setModalOpen] = useState(false);
  const [modalMode, setModalMode] = useState<'create' | 'rotate'>('create');
  const [modalExisting, setModalExisting] = useState<CredentialInfo | undefined>(undefined);

  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirm | null>(null);

  const refetch = useCallback(async () => {
    try {
      const list = await api.adminListCredentials();
      setCredentials(list);
      setListError(null);
    } catch (err) {
      setListError(errorMessage(err, 'Failed to load credentials.'));
    } finally {
      setLoading(false);
    }
  }, []);

  // Mount-only fetch, delegated to refetch (also used after every mutation
  // below) rather than duplicated inline — mirrors AdminUsersPage.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refetch();
  }, [refetch]);

  const openCreate = () => {
    setModalMode('create');
    setModalExisting(undefined);
    setModalOpen(true);
  };

  const openRotate = (credential: CredentialInfo) => {
    setModalMode('rotate');
    setModalExisting(credential);
    setModalOpen(true);
  };

  const setDisabled = useCallback(
    async (name: string, disabled: boolean) => {
      setActionError(null);
      try {
        await api.adminUpdateCredential(name, { disabled });
        await refetch();
      } catch (err) {
        setActionError(errorMessage(err, 'Action failed.'));
      }
    },
    [refetch],
  );

  const remove = useCallback(
    async (name: string) => {
      setActionError(null);
      try {
        await api.adminDeleteCredential(name);
        await refetch();
      } catch (err) {
        setActionError(errorMessage(err, 'Failed to delete credential.'));
      }
    },
    [refetch],
  );

  const confirmPending = async () => {
    if (!pendingConfirm) return;
    const { name, kind } = pendingConfirm;
    setPendingConfirm(null);
    if (kind === 'delete') {
      await remove(name);
    } else {
      await setDisabled(name, true);
    }
  };

  return (
    <div className="p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
          Credentials
        </h1>
        <button
          type="button"
          onClick={openCreate}
          className="rounded py-1.5 px-4 font-medium"
          style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)' }}
        >
          Add credential
        </button>
      </div>

      {actionError && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {actionError}
        </div>
      )}

      <CredentialsTable
        credentials={credentials}
        loading={loading}
        error={listError}
        onRotate={openRotate}
        onToggleDisabled={(c) =>
          c.disabled ? void setDisabled(c.name, false) : setPendingConfirm({ name: c.name, kind: 'disable' })
        }
        onDelete={(c) => setPendingConfirm({ name: c.name, kind: 'delete' })}
      />

      <CredentialModal
        open={modalOpen}
        mode={modalMode}
        existing={modalExisting}
        onClose={() => setModalOpen(false)}
        onSaved={() => void refetch()}
      />

      <ConfirmModal
        open={pendingConfirm !== null}
        title={pendingConfirm?.kind === 'delete' ? 'Delete credential?' : 'Disable credential?'}
        message={
          pendingConfirm?.kind === 'delete'
            ? `Deleting "${pendingConfirm.name}" is permanent. Any project still bound to it will fail closed.`
            : `Projects using "${pendingConfirm?.name}" will fail closed until it is re-enabled.`
        }
        variant="danger"
        confirmLabel={pendingConfirm?.kind === 'delete' ? 'Delete' : 'Disable'}
        onConfirm={() => void confirmPending()}
        onCancel={() => setPendingConfirm(null)}
      />
    </div>
  );
}
