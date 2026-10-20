import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { UserMenu } from './UserMenu';
import type { Palette } from '../../lib/palettes';

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
  palette: 'everforest' as Palette,
  setTheme: vi.fn(),
  setPalette: vi.fn(),
}));
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => themeState,
}));


const aboutState = vi.hoisted(() => ({
  hasUnseen: false,
  about: null,
  seen: {},
  unseen: {},
  refresh: vi.fn().mockResolvedValue(null),
  markSeen: vi.fn(),
}));
vi.mock('../../hooks/useAbout', () => ({ useAbout: () => aboutState }));
vi.mock('../About/AboutDialog', () => ({
  AboutDialog: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? (
      <div role="dialog" aria-label="About ContextMatrix">
        <button onClick={onClose}>Close about</button>
      </div>
    ) : null,
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
  aboutState.hasUnseen = false;
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
    expect(screen.getByRole('menuitemradio', { name: 'GitHub' })).toBeInTheDocument();
  });

  it('returns focus to the user chip when Escape closes the menu from inside it', () => {
    setAuth(false);
    render(<UserMenu />);
    const chip = screen.getByRole('button', { name: /Root/ });
    fireEvent.click(chip);
    screen.getByRole('menuitemradio', { name: 'Light' }).focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(chip);
  });
});

describe('UserMenu About ContextMatrix', () => {
  it.each([true, false])('puts About ContextMatrix directly above Change password (admin=%s)', (isAdmin) => {
    setAuth(isAdmin);
    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /Root/ }));
    const names = screen.getAllByRole('menuitem').map((el) => el.textContent?.trim());
    const about = names.findIndex((n) => n?.startsWith('About ContextMatrix'));
    expect(about).toBeGreaterThan(-1);
    expect(names[about + 1]).toBe('Change password');
  });

  it('opens the dialog and closes the menu', () => {
    setAuth(false);
    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /Root/ }));
    fireEvent.click(screen.getByRole('menuitem', { name: /About ContextMatrix/ }));
    expect(screen.queryByRole('menu')).toBeNull();
    expect(screen.getByRole('dialog', { name: 'About ContextMatrix' })).toBeInTheDocument();
  });

  it('returns focus to the trigger once the dialog closes', () => {
    setAuth(false);
    render(<UserMenu />);
    const trigger = screen.getByRole('button', { name: /Root/ });
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole('menuitem', { name: /About ContextMatrix/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Close about' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('shows the dot on the avatar and the item when there is news', () => {
    setAuth(false);
    aboutState.hasUnseen = true;
    render(<UserMenu />);
    const trigger = screen.getByRole('button', { name: /Root/ });
    expect(trigger).toHaveTextContent('(new changes)');
    fireEvent.click(trigger);
    expect(screen.getByRole('menuitem', { name: /About ContextMatrix/ })).toHaveTextContent('(new changes)');
  });

  it('shows no dot without news', () => {
    setAuth(false);
    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /Root/ }));
    expect(screen.queryByText('(new changes)')).toBeNull();
  });
});
