import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { HeaderCollapseToggle } from './HeaderCollapseToggle';

describe('HeaderCollapseToggle', () => {
  it('renders a collapse control when expanded and fires onToggle', () => {
    const onToggle = vi.fn();
    render(<HeaderCollapseToggle collapsed={false} onToggle={onToggle} />);

    const button = screen.getByRole('button', { name: /collapse board header/i });
    expect(button.getAttribute('aria-expanded')).toBe('true');

    fireEvent.click(button);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('renders an expand control when collapsed', () => {
    render(<HeaderCollapseToggle collapsed onToggle={vi.fn()} />);

    const button = screen.getByRole('button', { name: /expand board header/i });
    expect(button.getAttribute('aria-expanded')).toBe('false');
  });
});
