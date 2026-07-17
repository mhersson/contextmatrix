import { useState } from 'react';
import { api } from '../../api/client';
import { useAdminResource } from '../../hooks/useAdminResource';
import type { CredentialInfo } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { CredentialModal } from './CredentialModal';
import { CredentialsTable } from './CredentialsTable';

type ConfirmKind = 'disable' | 'delete';
interface PendingConfirm {
  name: string;
  kind: ConfirmKind;
}

const fetchCredentials = () => api.adminListCredentials();

/** Admin-only Credentials page: list, create/rotate (via CredentialModal),
 * and per-row disable/enable/delete management. Owns all data fetching and
 * mutations; the table and row it renders are purely presentational, and
 * CredentialModal owns its own create/rotate API call (see that file's
 * doc comment for why). */
export function AdminCredentialsPage() {
  const {
    items: credentials,
    loading,
    listError,
    actionError,
    refetch,
    act,
  } = useAdminResource(fetchCredentials, [], 'Failed to load credentials.');

  const [modalOpen, setModalOpen] = useState(false);
  const [modalMode, setModalMode] = useState<'create' | 'rotate'>('create');
  const [modalExisting, setModalExisting] = useState<CredentialInfo | undefined>(undefined);

  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirm | null>(null);

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

  const setDisabled = (name: string, disabled: boolean) =>
    act(() => api.adminUpdateCredential(name, { disabled }), 'Action failed.');

  const remove = (name: string) => act(() => api.adminDeleteCredential(name), 'Failed to delete credential.');

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
