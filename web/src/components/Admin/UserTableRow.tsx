import { chipTint } from '../../lib/chip';
import type { AdminUser } from '../../types';

interface UserTableRowProps {
  user: AdminUser;
  onToggleAdmin: () => void;
  onToggleDisabled: () => void;
  onRegenerateLink: () => void;
}

function formatLastLogin(ts?: string): string {
  return ts ? new Date(ts).toLocaleString() : 'Never';
}

/**
 * One row of the Users table. Purely presentational - AdminUsersPage
 * decides whether a toggle needs confirmation and owns the API calls;
 * this component just renders the row and reports clicks.
 */
export function UserTableRow({ user, onToggleAdmin, onToggleDisabled, onRegenerateLink }: UserTableRowProps) {
  return (
    <tr className="border-t" style={{ borderColor: 'var(--bg3)' }}>
      <td className="px-4 py-2 font-mono">{user.username}</td>
      <td className="px-4 py-2">
        {user.display_name}
        {!user.has_password && (
          <span className="chip-pill ml-2" style={chipTint('var(--yellow)')}>
            invite pending
          </span>
        )}
      </td>
      <td className="px-4 py-2">
        <span className="chip-pill" style={chipTint(user.is_admin ? 'var(--purple)' : 'var(--grey1)')}>
          {user.is_admin ? 'Admin' : 'User'}
        </span>
      </td>
      <td className="px-4 py-2">
        <span className="chip-pill" style={chipTint(user.disabled ? 'var(--red)' : 'var(--green)')}>
          {user.disabled ? 'Disabled' : 'Active'}
        </span>
      </td>
      <td className="px-4 py-2" style={{ color: 'var(--grey1)' }}>
        {formatLastLogin(user.last_login_at)}
      </td>
      <td className="px-4 py-2 text-right whitespace-nowrap">
        <button
          type="button"
          onClick={onToggleAdmin}
          className="text-xs px-2 py-1 rounded mr-1 hover:opacity-80"
          style={{ backgroundColor: 'var(--bg2)', color: 'var(--aqua)' }}
        >
          {user.is_admin ? 'Remove admin' : 'Make admin'}
        </button>
        <button
          type="button"
          onClick={onToggleDisabled}
          className="text-xs px-2 py-1 rounded mr-1 hover:opacity-80"
          style={{ backgroundColor: 'var(--bg2)', color: user.disabled ? 'var(--green)' : 'var(--orange)' }}
        >
          {user.disabled ? 'Enable' : 'Disable'}
        </button>
        <button
          type="button"
          onClick={onRegenerateLink}
          className="text-xs px-2 py-1 rounded hover:opacity-80"
          style={{ backgroundColor: 'var(--bg2)', color: 'var(--grey2)' }}
        >
          New link
        </button>
      </td>
    </tr>
  );
}
