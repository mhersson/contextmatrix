import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BoardFooter } from './BoardFooter';

describe('BoardFooter', () => {
  it('renders sync time + card count', () => {
    render(<BoardFooter lastSyncLabel="git sync · 18s ago" cardCount={32} columnCount={4} />);
    expect(screen.getByText(/git sync · 18s ago/)).toBeInTheDocument();
    expect(screen.getByText(/32 cards · 4 columns/)).toBeInTheDocument();
  });
});
