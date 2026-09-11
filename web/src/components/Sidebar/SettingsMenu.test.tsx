import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SettingsMenu } from './SettingsMenu';
import type { Palette } from '../../lib/palettes';

const navMock = vi.hoisted(() => vi.fn());
vi.mock('react-router', () => ({
  useNavigate: () => navMock,
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

beforeEach(() => {
  vi.clearAllMocks();
});

function openMenu() {
  const chip = screen.getByRole('button', { name: /settings/i });
  fireEvent.click(chip);
  return chip;
}

describe('SettingsMenu', () => {
  it('opens a menu with the none-mode admin pages and the appearance radios from a Settings chip', () => {
    render(<SettingsMenu />);
    expect(screen.queryByRole('menu')).toBeNull();

    const chip = screen.getByRole('button', { name: /settings/i });
    expect(chip).toHaveAttribute('aria-haspopup', 'menu');
    fireEvent.click(chip);

    expect(chip).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.getByText('ADMIN')).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Chats' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Model selection' })).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Catppuccin' })).toBeInTheDocument();
  });

  it('offers only the pages that exist without accounts', () => {
    render(<SettingsMenu />);
    openMenu();

    expect(screen.queryByRole('menuitem', { name: 'Users' })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: 'Credentials' })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: /sign out/i })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: /change password/i })).toBeNull();
  });

  it('navigates to an admin page, closes the menu and tells the caller', () => {
    const onNavigate = vi.fn();
    render(<SettingsMenu onNavigate={onNavigate} />);
    openMenu();

    fireEvent.click(screen.getByRole('menuitem', { name: 'Model selection' }));

    expect(navMock).toHaveBeenCalledWith('/admin/model-selection');
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('menu')).toBeNull();

    openMenu();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Chats' }));
    expect(navMock).toHaveBeenCalledWith('/admin/chats');
  });

  it('closes on Escape and on outside click', () => {
    render(
      <div>
        <SettingsMenu />
        <button type="button">outside</button>
      </div>
    );
    const chip = openMenu();
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();

    fireEvent.click(chip);
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('stays open after an appearance pick so theme and palette can be chosen together', () => {
    render(<SettingsMenu />);
    openMenu();
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Catppuccin' }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });

  it('returns focus to the chip when Escape closes the menu from inside it', () => {
    render(<SettingsMenu />);
    const chip = openMenu();
    screen.getByRole('menuitemradio', { name: 'Light' }).focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(chip);
  });

  it('returns focus to the chip when an outside click closes the menu from inside it', () => {
    render(
      <div>
        <SettingsMenu />
        <button type="button">outside</button>
      </div>
    );
    const chip = openMenu();
    screen.getByRole('menuitemradio', { name: 'Light' }).focus();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(chip);
  });
});
