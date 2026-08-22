import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { AppHeader } from './AppHeader';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({
    theme: 'dark',
    palette: 'everforest',
    version: '',
    taskBackend: '',
    chatEnabled: false,
    favorites: null,
    toggleTheme: vi.fn(),
    setPalette: vi.fn(),
  }),
}));

vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ toggle: vi.fn() }),
}));

function renderHeader(props: Partial<Parameters<typeof AppHeader>[0]> = {}) {
  return render(
    <MemoryRouter>
      <AppHeader project="demo" {...props} />
    </MemoryRouter>,
  );
}

describe('AppHeader - board header toggle', () => {
  it('renders an expanded toggle that collapses on click', () => {
    const onToggle = vi.fn();
    renderHeader({ headerCollapsed: false, onToggleHeaderCollapsed: onToggle });

    const button = screen.getByRole('button', { name: /collapse board header/i });
    expect(button.getAttribute('aria-expanded')).toBe('true');

    fireEvent.click(button);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('renders an expand toggle when the header is collapsed', () => {
    renderHeader({ headerCollapsed: true, onToggleHeaderCollapsed: vi.fn() });

    const button = screen.getByRole('button', { name: /expand board header/i });
    expect(button.getAttribute('aria-expanded')).toBe('false');
  });

  it('omits the toggle when no handler is provided', () => {
    renderHeader();
    expect(screen.queryByRole('button', { name: /board header/i })).toBeNull();
  });
});
