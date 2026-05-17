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
});
