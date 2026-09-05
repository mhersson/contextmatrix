import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SegmentedProgress } from './SegmentedProgress';

describe('SegmentedProgress', () => {
  it('tints the frontier segment and labels progress', () => {
    render(<SegmentedProgress segments={['complete', 'pending', 'pending']} />);
    const segs = screen.getByRole('img', { name: '1 of 3 complete' }).children;
    expect(segs[1]).toHaveClass('pb-seg-frontier');
    expect(segs[2]).not.toHaveClass('pb-seg-frontier');
  });

  it('leaves an active frontier in its agent color', () => {
    render(<SegmentedProgress segments={['active', 'pending']} />);
    const segs = screen.getByRole('img').children;
    expect(segs[0]).not.toHaveClass('pb-seg-frontier');
    expect(segs[1]).not.toHaveClass('pb-seg-frontier');
  });
});
