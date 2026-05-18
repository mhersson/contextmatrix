import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SummaryCards } from './SummaryCards';

describe('SummaryCards — Done Today tile', () => {
  it('renders completedToday as the headline when completedTodayParents is undefined', () => {
    render(
      <SummaryCards
        stateCounts={{ todo: 0, in_progress: 0, done: 0 }}
        totalCost={0}
        completedToday={7}
      />,
    );
    expect(screen.getByText('Done Today')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
    // No "+N sub" suffix without parents data.
    expect(screen.queryByText(/sub$/)).toBeNull();
  });

  it('uses completedTodayParents as headline and shows "+N sub" suffix when subtasks exist', () => {
    render(
      <SummaryCards
        stateCounts={{ todo: 0, in_progress: 0, done: 0 }}
        totalCost={0}
        completedToday={10}
        completedTodayParents={3}
      />,
    );
    // Headline = 3 (parents only).
    expect(screen.getByText('3')).toBeInTheDocument();
    // Suffix shows the 7 subtask completions.
    expect(screen.getByText('+7 sub')).toBeInTheDocument();
  });

  it('omits the suffix when completedTodayParents equals completedToday (no subtasks)', () => {
    render(
      <SummaryCards
        stateCounts={{ todo: 0, in_progress: 0, done: 0 }}
        totalCost={0}
        completedToday={4}
        completedTodayParents={4}
      />,
    );
    expect(screen.getByText('4')).toBeInTheDocument();
    expect(screen.queryByText(/sub$/)).toBeNull();
  });

  it('omits the suffix when both values are zero', () => {
    render(
      <SummaryCards
        stateCounts={{ todo: 0, in_progress: 0, done: 0 }}
        totalCost={0}
        completedToday={0}
        completedTodayParents={0}
      />,
    );
    expect(screen.queryByText(/sub$/)).toBeNull();
  });

  it('renders headline 0 with "+N sub" when only subtasks completed today (parents=0)', () => {
    // Guards against a regression from `??` to `||` — `||` would mishandle
    // the parents=0 case by falling through to `completedToday`.
    render(
      <SummaryCards
        stateCounts={{ todo: 0, in_progress: 0, done: 0 }}
        totalCost={0}
        completedToday={5}
        completedTodayParents={0}
      />,
    );
    const tile = screen.getByText('Done Today').closest('div')?.parentElement;
    expect(tile).not.toBeNull();
    // Headline is "0" (parent-only), suffix shows "+5 sub".
    expect(tile!.textContent).toMatch(/^0\+5 sub/);
  });
});
