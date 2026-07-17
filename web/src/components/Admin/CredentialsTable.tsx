import type { CredentialInfo } from '../../types';
import { AdminTable, type AdminTableHeader } from './AdminTable';
import { CredentialTableRow } from './CredentialTableRow';

interface CredentialsTableProps {
  credentials: CredentialInfo[];
  loading: boolean;
  error: string | null;
  onRotate: (credential: CredentialInfo) => void;
  onToggleDisabled: (credential: CredentialInfo) => void;
  onDelete: (credential: CredentialInfo) => void;
}

const HEADERS: AdminTableHeader[] = [
  { label: 'Name' },
  { label: 'Kind' },
  { label: 'Host' },
  { label: 'App ID' },
  { label: 'Installation ID' },
  { label: 'Created by' },
  { label: 'Last used' },
  { label: 'Status' },
  { label: 'Actions', align: 'right' },
];

export function CredentialsTable({
  credentials,
  loading,
  error,
  onRotate,
  onToggleDisabled,
  onDelete,
}: CredentialsTableProps) {
  return (
    <AdminTable
      loading={loading}
      error={error}
      empty={credentials.length === 0}
      emptyMessage="No credentials yet."
      headers={HEADERS}
    >
      {credentials.map((c) => (
        <CredentialTableRow
          key={c.name}
          credential={c}
          onRotate={() => onRotate(c)}
          onToggleDisabled={() => onToggleDisabled(c)}
          onDelete={() => onDelete(c)}
        />
      ))}
    </AdminTable>
  );
}
