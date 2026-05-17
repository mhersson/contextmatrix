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
});
