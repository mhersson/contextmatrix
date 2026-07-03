import type { AdminUser } from '../../types';
import { UserTableRow } from './UserTableRow';

interface UsersTableProps {
  users: AdminUser[];
  loading: boolean;
  error: string | null;
  onToggleAdmin: (user: AdminUser) => void;
  onToggleDisabled: (user: AdminUser) => void;
  onRegenerateLink: (username: string) => void;
}

/**
 * Loading/error/empty-state wrapper and `<table>` markup for the Users
 * page. Purely presentational — AdminUsersPage owns all data fetching,
 * action logic, and the decision of what a row toggle should do; this
 * component only renders the current state and threads row callbacks
 * through to `UserTableRow`.
 */
export function UsersTable({
  users,
  loading,
  error,
  onToggleAdmin,
  onToggleDisabled,
  onRegenerateLink,
}: UsersTableProps) {
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
      ) : users.length === 0 ? (
        <div className="p-6 text-sm" style={{ color: 'var(--grey0)' }}>
          No users yet.
        </div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="w-full text-sm" style={{ color: 'var(--fg)' }}>
            <thead>
              <tr style={{ color: 'var(--grey2)' }}>
                <th className="text-left px-4 py-2 font-medium">Username</th>
                <th className="text-left px-4 py-2 font-medium">Display name</th>
                <th className="text-left px-4 py-2 font-medium">Role</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
                <th className="text-left px-4 py-2 font-medium">Last login</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <UserTableRow
                  key={u.username}
                  user={u}
                  onToggleAdmin={() => onToggleAdmin(u)}
                  onToggleDisabled={() => onToggleDisabled(u)}
                  onRegenerateLink={() => onRegenerateLink(u.username)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
