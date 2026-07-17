import type { CredentialInfo } from '../../types';
import { CredentialTableRow } from './CredentialTableRow';

interface CredentialsTableProps {
  credentials: CredentialInfo[];
  loading: boolean;
  error: string | null;
  onRotate: (credential: CredentialInfo) => void;
  onToggleDisabled: (credential: CredentialInfo) => void;
  onDelete: (credential: CredentialInfo) => void;
}

/**
 * Loading/error/empty-state wrapper and `<table>` markup for the
 * Credentials page. Purely presentational - AdminCredentialsPage owns all
 * data fetching, action logic, and the decision of what a row action should
 * do; this component only renders the current state and threads row
 * callbacks through to `CredentialTableRow`.
 */
export function CredentialsTable({
  credentials,
  loading,
  error,
  onRotate,
  onToggleDisabled,
  onDelete,
}: CredentialsTableProps) {
  return (
    <div
      className="rounded-lg border overflow-hidden"
      style={{ backgroundColor: 'var(--bg1)', borderColor: 'var(--bg3)' }}
    >
      {loading ? (
        <div className="p-6 text-sm" style={{ color: 'var(--grey1)' }}>
          Loading…
        </div>
      ) : error ? (
        <div className="p-6 text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {error}
        </div>
      ) : credentials.length === 0 ? (
        <div className="p-6 text-sm" style={{ color: 'var(--grey0)' }}>
          No credentials yet.
        </div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="w-full text-sm" style={{ color: 'var(--fg)' }}>
            <thead>
              <tr style={{ color: 'var(--grey2)' }}>
                <th className="text-left px-4 py-2 font-medium">Name</th>
                <th className="text-left px-4 py-2 font-medium">Kind</th>
                <th className="text-left px-4 py-2 font-medium">Host</th>
                <th className="text-left px-4 py-2 font-medium">App ID</th>
                <th className="text-left px-4 py-2 font-medium">Installation ID</th>
                <th className="text-left px-4 py-2 font-medium">Created by</th>
                <th className="text-left px-4 py-2 font-medium">Last used</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {credentials.map((c) => (
                <CredentialTableRow
                  key={c.name}
                  credential={c}
                  onRotate={() => onRotate(c)}
                  onToggleDisabled={() => onToggleDisabled(c)}
                  onDelete={() => onDelete(c)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
