import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AppearanceMenuItems } from './AppearanceMenuItems';

const themeState = vi.hoisted(() => ({
  theme: 'dark' as 'dark' | 'light',
  palette: 'everforest' as 'everforest' | 'radix' | 'catppuccin',
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
    themeState.palette = 'radix';
    render(<AppearanceMenuItems />);

    expect(screen.getByRole('menuitemradio', { name: 'Light' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('menuitemradio', { name: 'Radix' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('menuitemradio', { name: 'Everforest' })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('menuitemradio', { name: 'Catppuccin' })).toHaveAttribute('aria-checked', 'false');
  });

  it('selecting a theme calls setTheme with that mode', () => {
    render(<AppearanceMenuItems />);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Light' }));
    expect(themeState.setTheme).toHaveBeenCalledWith('light');
    expect(themeState.setPalette).not.toHaveBeenCalled();
  });

  it('selecting a palette calls setPalette with that palette', () => {
    render(<AppearanceMenuItems />);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Catppuccin' }));
    expect(themeState.setPalette).toHaveBeenCalledWith('catppuccin');
    expect(themeState.setTheme).not.toHaveBeenCalled();
  });
});
