import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { AdminUsersPage } from './AdminUsersPage';
import type { AdminUser } from '../../types';

const mocks = vi.hoisted(() => ({
  adminListUsers: vi.fn(),
  adminCreateUser: vi.fn(),
  adminPatchUser: vi.fn(),
  adminRegenerateLink: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminListUsers: mocks.adminListUsers,
      adminCreateUser: mocks.adminCreateUser,
      adminPatchUser: mocks.adminPatchUser,
      adminRegenerateLink: mocks.adminRegenerateLink,
    },
  };
});

function user(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    username: 'alice',
    display_name: 'Alice',
    is_admin: false,
    disabled: false,
    has_password: true,
    ...overrides,
  };
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.adminRegenerateLink.mockResolvedValue({ token: 'unused', purpose: 'invite', expires_at: '2026-01-01T00:00:00Z' });
});

describe('AdminUsersPage — list', () => {
  it('renders the user list from adminListUsers, with role/status chips and invite-pending badge', async () => {
    mocks.adminListUsers.mockResolvedValue([
      user({ username: 'alice', display_name: 'Alice', is_admin: true, has_password: true }),
      user({ username: 'bob', display_name: 'Bob', is_admin: false, has_password: false, disabled: true }),
    ]);

    render(<AdminUsersPage />);

    await waitFor(() => expect(screen.getByText('alice')).toBeInTheDocument());
    expect(screen.getByText('bob')).toBeInTheDocument();
    expect(screen.getByText('Admin')).toBeInTheDocument();
    expect(screen.getByText('Disabled')).toBeInTheDocument();
    expect(screen.getByText(/invite pending/i)).toBeInTheDocument();
  });
});

describe('AdminUsersPage — create flow', () => {
  it('calls adminCreateUser and shows the composed invite link', async () => {
    mocks.adminListUsers
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([user({ username: 'carol', display_name: '', has_password: false })]);
    mocks.adminCreateUser.mockResolvedValue({
      user: user({ username: 'carol', display_name: '', has_password: false }),
      invite: { token: 'tok-12345', purpose: 'invite', expires_at: '2026-01-03T00:00:00Z' },
    });

    render(<AdminUsersPage />);

    fireEvent.click(screen.getByRole('button', { name: 'New user' }));
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'carol' } });
    fireEvent.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() =>
      expect(mocks.adminCreateUser).toHaveBeenCalledWith({
        username: 'carol',
        display_name: undefined,
        is_admin: false,
      })
    );

    await waitFor(() => expect(screen.getByText(/\/auth\/token\/tok-12345/)).toBeInTheDocument());
    expect(screen.getByText(/48/)).toBeInTheDocument();
  });
});

describe('AdminUsersPage — last-admin guard', () => {
  it('surfaces a 409 from adminPatchUser as an inline error without crashing', async () => {
    mocks.adminListUsers.mockResolvedValue([
      user({ username: 'root', display_name: 'Root', is_admin: true, has_password: true }),
    ]);
    mocks.adminPatchUser.mockRejectedValue({ code: 'VALIDATION_ERROR', error: 'cannot remove the last admin' });

    render(<AdminUsersPage />);

    await waitFor(() => expect(screen.getByText('root')).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: /remove admin/i }));

    const dialog = screen.getByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /remove admin/i }));

    await waitFor(() => expect(mocks.adminPatchUser).toHaveBeenCalledWith('root', { is_admin: false }));
    await waitFor(() => expect(screen.getByText(/cannot remove the last admin/i)).toBeInTheDocument());

    // Component survives the error — the row is still rendered, not crashed.
    expect(screen.getByText('root')).toBeInTheDocument();
  });
});
