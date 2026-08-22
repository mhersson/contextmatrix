import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AppearanceMenu } from './AppearanceMenu';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', palette: 'everforest', setTheme: vi.fn(), setPalette: vi.fn() }),
}));

describe('AppearanceMenu', () => {
  it('opens a menu with the appearance radios from an Appearance chip', () => {
    render(<AppearanceMenu />);
    expect(screen.queryByRole('menu')).toBeNull();

    const chip = screen.getByRole('button', { name: /appearance/i });
    expect(chip).toHaveAttribute('aria-haspopup', 'menu');
    fireEvent.click(chip);

    expect(chip).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Catppuccin' })).toBeInTheDocument();
  });

  it('closes on Escape and on outside click', () => {
    render(
      <div>
        <AppearanceMenu />
        <button type="button">outside</button>
      </div>
    );
    const chip = screen.getByRole('button', { name: /appearance/i });

    fireEvent.click(chip);
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();

    fireEvent.click(chip);
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('stays open after a pick so theme and palette can be chosen together', () => {
    render(<AppearanceMenu />);
    fireEvent.click(screen.getByRole('button', { name: /appearance/i }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Catppuccin' }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });

  it('returns focus to the chip when Escape closes the menu from inside it', () => {
    render(<AppearanceMenu />);
    const chip = screen.getByRole('button', { name: /appearance/i });
    fireEvent.click(chip);
    screen.getByRole('menuitemradio', { name: 'Light' }).focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(chip);
  });

  it('returns focus to the chip when an outside click closes the menu from inside it', () => {
    render(
      <div>
        <AppearanceMenu />
        <button type="button">outside</button>
      </div>
    );
    const chip = screen.getByRole('button', { name: /appearance/i });
    fireEvent.click(chip);
    screen.getByRole('menuitemradio', { name: 'Light' }).focus();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(chip);
  });
});
