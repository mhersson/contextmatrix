import type { AdminUser } from '../../types';
import { AdminTable, type AdminTableHeader } from './AdminTable';
import { UserTableRow } from './UserTableRow';

interface UsersTableProps {
  users: AdminUser[];
  loading: boolean;
  error: string | null;
  onToggleAdmin: (user: AdminUser) => void;
  onToggleDisabled: (user: AdminUser) => void;
  onRegenerateLink: (username: string) => void;
}

const HEADERS: AdminTableHeader[] = [
  { label: 'Username' },
  { label: 'Display name' },
  { label: 'Role' },
  { label: 'Status' },
  { label: 'Last login' },
  { label: 'Actions', align: 'right' },
];

export function UsersTable({
  users,
  loading,
  error,
  onToggleAdmin,
  onToggleDisabled,
  onRegenerateLink,
}: UsersTableProps) {
  return (
    <AdminTable
      loading={loading}
      error={error}
      empty={users.length === 0}
      emptyMessage="No users yet."
      headers={HEADERS}
    >
      {users.map((u) => (
        <UserTableRow
          key={u.username}
          user={u}
          onToggleAdmin={() => onToggleAdmin(u)}
          onToggleDisabled={() => onToggleDisabled(u)}
          onRegenerateLink={() => onRegenerateLink(u.username)}
        />
      ))}
    </AdminTable>
  );
}
