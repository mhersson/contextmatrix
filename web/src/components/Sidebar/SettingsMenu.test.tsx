import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
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

beforeEach(() => {
  aboutState.hasUnseen = false;
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
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();

    const chip = screen.getByRole('button', { name: /settings/i });
    fireEvent.click(chip);

    expect(chip).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('group', { name: 'Settings menu' })).toBeInTheDocument();
    expect(screen.getByText('ADMIN')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Chats' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Model selection' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Dark' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Catppuccin' })).toBeInTheDocument();
  });

  it('offers only the pages that exist without accounts', () => {
    render(<SettingsMenu />);
    openMenu();

    expect(screen.queryByRole('button', { name: 'Users' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Credentials' })).toBeNull();
    expect(screen.queryByRole('button', { name: /sign out/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /change password/i })).toBeNull();
  });

  it('navigates to an admin page, closes the menu and tells the caller', () => {
    const onNavigate = vi.fn();
    render(<SettingsMenu onNavigate={onNavigate} />);
    openMenu();

    fireEvent.click(screen.getByRole('button', { name: 'Model selection' }));

    expect(navMock).toHaveBeenCalledWith('/admin/model-selection');
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();

    openMenu();
    fireEvent.click(screen.getByRole('button', { name: 'Chats' }));
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
    expect(screen.getByRole('group', { name: 'Settings menu' })).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();

    fireEvent.click(chip);
    expect(screen.getByRole('group', { name: 'Settings menu' })).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();
  });

  it('stays open after an appearance pick so theme and palette can be chosen together', () => {
    render(<SettingsMenu />);
    openMenu();
    fireEvent.click(screen.getByRole('button', { name: 'Catppuccin' }));
    expect(screen.getByRole('group', { name: 'Settings menu' })).toBeInTheDocument();
  });

  it('returns focus to the chip when Escape closes the menu from inside it', () => {
    render(<SettingsMenu />);
    const chip = openMenu();
    screen.getByRole('button', { name: 'Light' }).focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();
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
    screen.getByRole('button', { name: 'Light' }).focus();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();
    expect(document.activeElement).toBe(chip);
  });
});

describe('SettingsMenu About ContextMatrix', () => {
  it('offers About ContextMatrix as the last item', () => {
    render(<SettingsMenu />);
    openMenu();
    const items = screen.getAllByRole('button');
    expect(items[items.length - 1]).toHaveTextContent('About ContextMatrix');
  });

  it('returns focus to the trigger once the dialog closes', () => {
    render(<SettingsMenu />);
    const trigger = openMenu();
    fireEvent.click(screen.getByRole('button', { name: /About ContextMatrix/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Close about' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('opens the dialog and closes the menu', () => {
    render(<SettingsMenu />);
    openMenu();
    fireEvent.click(screen.getByRole('button', { name: /About ContextMatrix/ }));
    expect(screen.queryByRole('group', { name: 'Settings menu' })).toBeNull();
    expect(screen.getByRole('dialog', { name: 'About ContextMatrix' })).toBeInTheDocument();
  });

  it('shows the dot on the gear and the item when there is news', () => {
    aboutState.hasUnseen = true;
    render(<SettingsMenu />);
    const chip = openMenu();
    expect(within(chip).getByTestId('new-dot')).toBeInTheDocument();
    expect(within(screen.getByRole('button', { name: /About ContextMatrix/ })).getByTestId('new-dot')).toBeInTheDocument();
  });

  it('shows no dot without news', () => {
    render(<SettingsMenu />);
    openMenu();
    expect(screen.queryByTestId('new-dot')).toBeNull();
  });
});
