import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { UserMenu } from './UserMenu';

const navMock = vi.hoisted(() => vi.fn());
vi.mock('react-router', () => ({
  useNavigate: () => navMock,
}));

const authState = vi.hoisted(() => ({
  current: null as unknown,
}));
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

const themeState = vi.hoisted(() => ({
  theme: 'dark' as 'dark' | 'light',
  palette: 'everforest' as 'everforest' | 'radix' | 'catppuccin',
  setTheme: vi.fn(),
  setPalette: vi.fn(),
}));
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => themeState,
}));

function setAuth(isAdmin: boolean) {
  authState.current = {
    mode: 'multi',
    user: { username: 'root', display_name: 'Root', is_admin: isAdmin },
    logout: vi.fn(),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('UserMenu admin section', () => {
  it('shows Users/Credentials/Chats for admins and navigates on click', () => {
    setAuth(true);
    const onNavigate = vi.fn();
    render(<UserMenu onNavigate={onNavigate} />);

    fireEvent.click(screen.getByRole('button', { name: /Root/ }));

    expect(screen.getByText('ADMIN')).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Users' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Credentials' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('menuitem', { name: 'Chats' }));
    expect(navMock).toHaveBeenCalledWith('/admin/chats');
    expect(onNavigate).toHaveBeenCalled();
    // Menu closed after navigation:
    expect(screen.queryByRole('menuitem', { name: 'Users' })).not.toBeInTheDocument();
  });

  it('hides the admin section for non-admins', () => {
    setAuth(false);
    render(<UserMenu />);

    fireEvent.click(screen.getByRole('button', { name: /Root/ }));

    expect(screen.queryByText('ADMIN')).not.toBeInTheDocument();
    expect(screen.queryByRole('menuitem', { name: 'Users' })).not.toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Change password' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Sign out' })).toBeInTheDocument();
  });
});

describe('UserMenu appearance section', () => {
  it('offers theme and palette radios between the admin section and account actions', () => {
    setAuth(true);
    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /Root/ }));

    const admin = screen.getByText('ADMIN');
    const appearance = screen.getByText('APPEARANCE');
    const changePw = screen.getByRole('menuitem', { name: 'Change password' });
    expect(admin.compareDocumentPosition(appearance) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(appearance.compareDocumentPosition(changePw) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('menuitemradio', { name: 'Everforest' })).toHaveAttribute('aria-checked', 'true');
  });

  it('shows the appearance section to non-admins too and keeps the menu open after a pick', () => {
    setAuth(false);
    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /Root/ }));

    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Light' }));
    expect(themeState.setTheme).toHaveBeenCalledWith('light');
    expect(screen.getByRole('menuitemradio', { name: 'Radix' })).toBeInTheDocument();
  });
});
