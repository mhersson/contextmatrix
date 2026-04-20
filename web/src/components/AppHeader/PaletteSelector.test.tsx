import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { PaletteSelector } from './PaletteSelector';

const mockSetPalette = vi.fn();

vi.mock('../../hooks/useTheme', () => ({
  useTheme: vi.fn(() => ({
    theme: 'dark',
    palette: 'everforest',
    version: '',
    toggleTheme: vi.fn(),
    setPalette: mockSetPalette,
  })),
}));

import { useTheme } from '../../hooks/useTheme';

const mockUseTheme = vi.mocked(useTheme);

describe('PaletteSelector', () => {
  beforeEach(() => {
    mockSetPalette.mockClear();
    mockUseTheme.mockReturnValue({
      theme: 'dark',
      palette: 'everforest',
      version: '',
      toggleTheme: vi.fn(),
      setPalette: mockSetPalette,
    });
  });

  it('renders the toggle button', () => {
    render(<PaletteSelector />);
    const button = screen.getByRole('button', { name: /palette/i });
    expect(button).toBeTruthy();
  });

  it('does not show palette options when closed', () => {
    render(<PaletteSelector />);
    expect(screen.queryByText('Everforest')).toBeNull();
    expect(screen.queryByText('Radix')).toBeNull();
    expect(screen.queryByText('Catppuccin')).toBeNull();
  });

  it('renders all three palette options when open', () => {
    render(<PaletteSelector />);

    fireEvent.click(screen.getByRole('button', { name: /palette/i }));

    expect(screen.getByText('Everforest')).toBeTruthy();
    expect(screen.getByText('Radix')).toBeTruthy();
    expect(screen.getByText('Catppuccin')).toBeTruthy();
  });

  it('marks the active palette with a checkmark', () => {
    mockUseTheme.mockReturnValue({
      theme: 'dark',
      palette: 'radix',
      version: '',
      toggleTheme: vi.fn(),
      setPalette: mockSetPalette,
    });

    render(<PaletteSelector />);
    fireEvent.click(screen.getByRole('button', { name: /palette/i }));

    const radixOption = screen.getByText('Radix').closest('button');
    expect(radixOption?.textContent).toContain('✓');

    const everforestOption = screen.getByText('Everforest').closest('button');
    expect(everforestOption?.textContent).not.toContain('✓');
  });

  it('calls setPalette with the clicked palette', () => {
    render(<PaletteSelector />);

    fireEvent.click(screen.getByRole('button', { name: /palette/i }));
    fireEvent.click(screen.getByText('Catppuccin'));

    expect(mockSetPalette).toHaveBeenCalledWith('catppuccin');
    expect(mockSetPalette).toHaveBeenCalledTimes(1);
  });

  it('closes the menu after a palette is selected', () => {
    render(<PaletteSelector />);

    fireEvent.click(screen.getByRole('button', { name: /palette/i }));
    expect(screen.getByText('Everforest')).toBeTruthy();

    fireEvent.click(screen.getByText('Radix'));
    expect(screen.queryByText('Everforest')).toBeNull();
  });

  it('closes the menu on Escape key', () => {
    render(<PaletteSelector />);

    fireEvent.click(screen.getByRole('button', { name: /palette/i }));
    expect(screen.getByText('Everforest')).toBeTruthy();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText('Everforest')).toBeNull();
  });

  it('closes the menu on outside click', () => {
    render(
      <div>
        <PaletteSelector />
        <button type="button">outside</button>
      </div>
    );

    fireEvent.click(screen.getByRole('button', { name: /palette/i }));
    expect(screen.getByText('Everforest')).toBeTruthy();

    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByText('Everforest')).toBeNull();
  });
});
