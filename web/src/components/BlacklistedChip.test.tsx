import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BlacklistedChip } from './BlacklistedChip';

describe('BlacklistedChip', () => {
  it('renders the blacklisted label with the meaning in its tooltip', () => {
    render(<BlacklistedChip />);
    const chip = screen.getByText('blacklisted');
    expect(chip).toHaveAttribute(
      'title',
      'Reported incapable; a pin overrides the blacklist',
    );
  });

  it('is not an interactive control', () => {
    render(<BlacklistedChip />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.queryByRole('option')).not.toBeInTheDocument();
  });
});
