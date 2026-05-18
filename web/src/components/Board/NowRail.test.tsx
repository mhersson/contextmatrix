import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { NowRail } from './NowRail';
import type { ActiveAgent } from '../../types';

describe('NowRail', () => {
  const agents: ActiveAgent[] = [
    { agent_id: 'claude-haiku-4.5', card_id: 'CTX-308', card_title: 'SSE bus integration', since: '2026-05-17T12:00:00Z', last_heartbeat: '2026-05-17T12:00:00Z' },
    { agent_id: 'claude-sonnet-4.6', card_id: 'CTX-305', card_title: 'Parent transition', since: '2026-05-17T11:30:00Z', last_heartbeat: '2026-05-17T11:30:00Z' },
  ];

  it('lists the active agents with their card refs', () => {
    render(<NowRail agents={agents} activityEntries={[]} />);
    expect(screen.getByText('haiku-4.5')).toBeInTheDocument();
    expect(screen.getByText('CTX-308')).toBeInTheDocument();
    expect(screen.getByText('sonnet-4.6')).toBeInTheDocument();
    expect(screen.getByText('CTX-305')).toBeInTheDocument();
  });

  it('shows the agent count in the heading', () => {
    render(<NowRail agents={agents} activityEntries={[]} />);
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('uses "since page load" label for the activity section', () => {
    render(<NowRail agents={agents} activityEntries={[]} />);
    expect(screen.getByText(/since page load/i)).toBeInTheDocument();
  });

  it('renders activity entries with relative time', () => {
    render(
      <NowRail
        agents={[]}
        activityEntries={[
          { id: 'e1', agent: 'haiku-4.5', action: 'claim', cardId: 'CTX-308', ts: '2026-05-17T12:00:00Z' },
        ]}
      />
    );
    expect(screen.getByText('haiku-4.5')).toBeInTheDocument();
  });

  it('renders capacity X / max containers when maxAgents and runningContainers are provided', () => {
    render(<NowRail agents={agents} activityEntries={[]} maxAgents={8} runningContainers={3} />);
    // Capacity meta strip shows container count, not agent count.
    expect(screen.getByText(/3 \/ 8 containers/)).toBeInTheDocument();
    // 3/8 = 37.5 → rounds to 38%
    expect(screen.getByText(/38%/)).toBeInTheDocument();
  });

  it('renders capacity head-row with runningContainers count independent of agents length', () => {
    render(<NowRail agents={agents} activityEntries={[]} maxAgents={8} runningContainers={3} />);
    // Head-row shows "3 running" (runningContainers), not "2 running" (agents.length).
    expect(screen.getByText('3 running')).toBeInTheDocument();
  });

  it('renders capacity section in degraded form when maxAgents is absent', () => {
    render(<NowRail agents={agents} activityEntries={[]} runningContainers={2} />);
    expect(screen.getByText('Capacity')).toBeInTheDocument();
    expect(screen.getByText(/no cap set/i)).toBeInTheDocument();
    // Fallback uses container count, not agents.length-based copy.
    expect(screen.getByText(/2 active · no cap set/i)).toBeInTheDocument();
  });

  it('caps the activity feed at 8 entries', () => {
    const entries = Array.from({ length: 15 }, (_, i) => ({
      id: `e${i}`,
      agent: `agent-${i}`,
      action: 'claim',
      cardId: `CTX-${i}`,
      ts: '2026-05-17T12:00:00Z',
    }));
    render(<NowRail agents={[]} activityEntries={entries} />);
    // Each entry renders its cardId text; we should see the first 8 only.
    expect(screen.getByText('CTX-0')).toBeInTheDocument();
    expect(screen.getByText('CTX-7')).toBeInTheDocument();
    expect(screen.queryByText('CTX-8')).not.toBeInTheDocument();
  });

  it('switches activity label to "Activity" when hasBackfill is true', () => {
    render(<NowRail agents={agents} activityEntries={[]} hasBackfill={true} />);
    expect(screen.queryByText(/since page load/i)).not.toBeInTheDocument();
    expect(screen.getByText('Activity')).toBeInTheDocument();
  });
});
