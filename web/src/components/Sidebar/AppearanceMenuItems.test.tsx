import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { AppearanceMenuItems } from './AppearanceMenuItems';
import type { Palette } from '../../lib/palettes';

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
  themeState.theme = 'dark';
  themeState.palette = 'everforest';
  themeState.setTheme.mockClear();
  themeState.setPalette.mockClear();
});

describe('AppearanceMenuItems', () => {
  it('marks the active theme and palette as checked', () => {
    themeState.theme = 'light';
    themeState.palette = 'github';
    render(<AppearanceMenuItems />);

    expect(screen.getByRole('button', { name: 'Light' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Dark' })).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: 'GitHub' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Everforest' })).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: 'Catppuccin' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('selecting a theme calls setTheme with that mode', () => {
    render(<AppearanceMenuItems />);
    fireEvent.click(screen.getByRole('button', { name: 'Light' }));
    expect(themeState.setTheme).toHaveBeenCalledWith('light');
    expect(themeState.setPalette).not.toHaveBeenCalled();
  });

  it('offers everforest, catppuccin, github and ayu, and no longer radix', () => {
    render(<AppearanceMenuItems />);
    const palette = screen.getByRole('group', { name: 'Palette' });
    const names = within(palette).getAllByRole('button').map((el) => el.textContent?.replace('✓', '').trim());
    expect(names).toEqual(['Everforest', 'Catppuccin', 'GitHub', 'Ayu']);
    expect(screen.queryByRole('button', { name: 'Radix' })).toBeNull();
  });

  it('selecting a palette calls setPalette with that palette', () => {
    render(<AppearanceMenuItems />);
    fireEvent.click(screen.getByRole('button', { name: 'Catppuccin' }));
    expect(themeState.setPalette).toHaveBeenCalledWith('catppuccin');
    expect(themeState.setTheme).not.toHaveBeenCalled();
  });

  it('exposes theme and palette as separately labelled radio groups', () => {
    render(<AppearanceMenuItems />);
    const theme = screen.getByRole('group', { name: 'Theme' });
    const palette = screen.getByRole('group', { name: 'Palette' });
    expect(theme).toContainElement(screen.getByRole('button', { name: 'Dark' }));
    expect(palette).toContainElement(screen.getByRole('button', { name: 'GitHub' }));
    expect(theme).not.toContainElement(screen.getByRole('button', { name: 'GitHub' }));
  });
});
