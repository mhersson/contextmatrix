import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { RouteTrack } from './RouteTrack';
import type { PlaybookSegment } from '../../types';

function nodes() {
  return screen.getByRole('img').querySelectorAll('.pbl-node');
}

describe('RouteTrack', () => {
  it('renders one node per entry with its state and rings the frontier', () => {
    const segments: PlaybookSegment[] = ['complete', 'pending', 'missing', 'pending'];
    render(<RouteTrack segments={segments} gates={[1]} />);
    expect(screen.getByRole('img', { name: '1 of 4 complete' })).toBeInTheDocument();
    const list = nodes();
    expect(list).toHaveLength(4);
    expect(list[0]).toHaveClass('pbl-node-complete');
    expect(list[1]).toHaveClass('pbl-node-frontier');
    expect(list[1]).toHaveClass('pbl-node-gate');
    expect(list[2]).toHaveClass('pbl-node-missing');
    expect(list[3]).not.toHaveClass('pbl-node-frontier');
  });

  it('keeps the agent styling on an active frontier instead of the purple ring', () => {
    render(<RouteTrack segments={['complete', 'active', 'pending']} />);
    const list = nodes();
    expect(list[1]).toHaveClass('pbl-node-active');
    expect(list[1]).not.toHaveClass('pbl-node-frontier');
    expect(list[2]).not.toHaveClass('pbl-node-frontier');
  });

  it('colors the rail after a complete node and dashes it otherwise', () => {
    render(<RouteTrack segments={['complete', 'pending', 'pending']} />);
    const rails = screen.getByRole('img').querySelectorAll('.pbl-rail');
    expect(rails).toHaveLength(2);
    expect(rails[0]).toHaveClass('pbl-rail-on');
    expect(rails[1]).toHaveClass('pbl-rail-dash');
  });

  it('falls back to the segments strip above the node cap', () => {
    const segments: PlaybookSegment[] = Array.from({ length: 21 }, () => 'complete');
    render(<RouteTrack segments={segments} />);
    expect(nodes()).toHaveLength(0);
    expect(screen.getByRole('img', { name: '21 of 21 complete' })).toBeInTheDocument();
  });
});
