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

  it('falls back to degraded copy when maxAgents is set but runningContainers is undefined', () => {
    // Initial render before the first running-containers poll resolves: the
    // meter must NOT flash "0 / 8 containers · 0%". We fall through to the
    // "no cap set" degraded copy until both values are known.
    render(<NowRail agents={agents} activityEntries={[]} maxAgents={8} />);
    expect(screen.getByText(/no cap set/i)).toBeInTheDocument();
    expect(screen.queryByText(/0 \/ 8 containers/)).not.toBeInTheDocument();
  });

  it('clamps negative runningContainers to 0 in the meter', () => {
    render(<NowRail agents={agents} activityEntries={[]} maxAgents={8} runningContainers={-3} />);
    expect(screen.getByText(/0 \/ 8 containers/)).toBeInTheDocument();
    expect(screen.getByText('0%')).toBeInTheDocument();
    // Negative input must not leak into the meta strip.
    expect(screen.queryByText(/-3 \/ 8 containers/)).not.toBeInTheDocument();
  });

  it('clamps NaN runningContainers to 0 in the meter', () => {
    render(<NowRail agents={agents} activityEntries={[]} maxAgents={8} runningContainers={NaN} />);
    expect(screen.getByText(/0 \/ 8 containers/)).toBeInTheDocument();
    expect(screen.getByText('0%')).toBeInTheDocument();
    expect(screen.queryByText(/NaN/)).not.toBeInTheDocument();
  });

  it('does not divide the Now · agents head-row by the runner container cap', () => {
    // Fix B-1: the agents head-row must show just the active-agent count
    // (here: 2), never "2 / 8". The runner cap (max_concurrent) is a
    // container number — the canonical place for it is the Capacity meter.
    render(<NowRail agents={agents} activityEntries={[]} maxAgents={8} runningContainers={3} />);
    // Locate the head-row inside the "Now · agents" section.
    const agentsLabel = screen.getByText('Now · agents');
    const agentsHead = agentsLabel.parentElement!;
    const countSpan = agentsHead.querySelector('.count')!;
    expect(countSpan.textContent).toBe('2');
    expect(countSpan.textContent).not.toContain('/');
    // Sanity: the runner-cap denominator still lives in the Capacity meta.
    expect(screen.getByText(/3 \/ 8 containers/)).toBeInTheDocument();
  });
});
