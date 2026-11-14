import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { WaitBadge } from './WaitBadge';

describe('WaitBadge', () => {
  it('gives the bare badge a spoken name instead of the "!" glyph', () => {
    render(<button type="button"><WaitBadge /></button>);
    const button = screen.getByRole('button');
    expect(button).toHaveAccessibleName();
    expect(button).not.toHaveAccessibleName(/!/);
  });

  it('reads a counted badge as its count', () => {
    render(<button type="button"><WaitBadge count={3} /></button>);
    expect(screen.getByRole('button')).toHaveAccessibleName('3');
  });
});
