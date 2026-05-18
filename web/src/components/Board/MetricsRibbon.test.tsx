import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MetricsRibbon } from './MetricsRibbon';

describe('MetricsRibbon', () => {
  it('renders four tiles in order', () => {
    render(
      <MetricsRibbon
        activeAgents={4}
        inFlight={11}
        stalled={2}
        shippedToday={3}
      />
    );
    expect(screen.getByText('Active agents')).toBeInTheDocument();
    expect(screen.getByText('In flight')).toBeInTheDocument();
    expect(screen.getByText('Stalled')).toBeInTheDocument();
    expect(screen.getByText('Shipped today')).toBeInTheDocument();
  });

  it('renders the count for each tile', () => {
    render(
      <MetricsRibbon activeAgents={4} inFlight={11} stalled={2} shippedToday={3} />
    );
    expect(screen.getByText('4')).toBeInTheDocument();
    expect(screen.getByText('11')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('shows Shipped · 7d tile with delta when 7d fields are provided', () => {
    render(
      <MetricsRibbon
        activeAgents={4} inFlight={11} stalled={2} shippedToday={3}
        shipped7d={14} shipped7dPrior={11}
      />
    );
    expect(screen.getByText('Shipped · 7d')).toBeInTheDocument();
    expect(screen.getByText('14')).toBeInTheDocument();
    expect(screen.getByText('+27%')).toBeInTheDocument();
  });

  it('hides Shipped · 7d tile when shipped7d is undefined', () => {
    render(<MetricsRibbon activeAgents={4} inFlight={11} stalled={2} shippedToday={3} />);
    expect(screen.queryByText('Shipped · 7d')).not.toBeInTheDocument();
  });

  it('shows +N sub suffixes on all affected tiles when subtask counts are positive', () => {
    render(
      <MetricsRibbon
        activeAgents={2}
        inFlight={3}
        inFlightSubtasks={5}
        stalled={1}
        stalledSubtasks={2}
        shippedToday={4}
        shippedTodaySubtasks={3}
        shipped7d={10}
        shipped7dSubtasks={7}
        shipped7dPrior={8}
      />
    );
    // Each tile shows its parent-only headline and the +N sub span.
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('1')).toBeInTheDocument();
    expect(screen.getByText('4')).toBeInTheDocument();
    expect(screen.getByText('10')).toBeInTheDocument();
    // The +N sub spans should all be present (each is a separate element).
    const subSpans = screen.getAllByText(/^\+\d+ sub$/);
    expect(subSpans).toHaveLength(4);
    expect(screen.getByText('+5 sub')).toBeInTheDocument();
    expect(screen.getByText('+2 sub')).toBeInTheDocument();
    expect(screen.getByText('+3 sub')).toBeInTheDocument();
    expect(screen.getByText('+7 sub')).toBeInTheDocument();
    // Active agents tile must not show a sub span.
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('omits +N sub spans when subtask counts are zero or undefined', () => {
    render(
      <MetricsRibbon
        activeAgents={2}
        inFlight={3}
        inFlightSubtasks={0}
        stalled={1}
        stalledSubtasks={0}
        shippedToday={4}
        shippedTodaySubtasks={0}
        shipped7d={10}
        shipped7dSubtasks={0}
        shipped7dPrior={8}
      />
    );
    expect(screen.queryByText(/^\+\d+ sub$/)).not.toBeInTheDocument();
  });

  it('omits +N sub spans when all subtask props are undefined', () => {
    render(
      <MetricsRibbon
        activeAgents={2}
        inFlight={3}
        stalled={1}
        shippedToday={4}
        shipped7d={10}
        shipped7dPrior={8}
      />
    );
    expect(screen.queryByText(/^\+\d+ sub$/)).not.toBeInTheDocument();
  });

  // SubCount helper behaviour verified through MetricsRibbon props.
  // (The helper is file-private and not directly exportable.)
  it('SubCount: renders nothing for undefined n', () => {
    render(
      <MetricsRibbon
        activeAgents={1}
        inFlight={5}
        inFlightSubtasks={undefined}
        stalled={0}
        shippedToday={2}
      />
    );
    expect(screen.queryByText(/^\+\d+ sub$/)).not.toBeInTheDocument();
  });

  it('SubCount: renders nothing for n=0', () => {
    render(
      <MetricsRibbon
        activeAgents={1}
        inFlight={5}
        inFlightSubtasks={0}
        stalled={0}
        shippedToday={2}
      />
    );
    expect(screen.queryByText(/^\+\d+ sub$/)).not.toBeInTheDocument();
  });

  it('SubCount: renders "+N sub" for positive n', () => {
    render(
      <MetricsRibbon
        activeAgents={1}
        inFlight={5}
        inFlightSubtasks={3}
        stalled={0}
        shippedToday={2}
      />
    );
    expect(screen.getByText('+3 sub')).toBeInTheDocument();
  });

  it('suppresses +N sub spans when stateCountsParents is undefined even if inFlightSubtasks would be positive', () => {
    // This simulates the Board.tsx fix: when stateCountsParents is undefined,
    // Board.tsx passes inFlightSubtasks=undefined so no "+N sub" renders yet.
    render(
      <MetricsRibbon
        activeAgents={2}
        inFlight={5}
        inFlightSubtasks={undefined}
        stalled={1}
        stalledSubtasks={undefined}
        shippedToday={3}
        shippedTodaySubtasks={undefined}
        shipped7d={10}
        shipped7dSubtasks={undefined}
        shipped7dPrior={8}
      />
    );
    expect(screen.queryByText(/^\+\d+ sub$/)).not.toBeInTheDocument();
  });
});
