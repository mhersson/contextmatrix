import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { AttentionItem, ChatSession, DashboardData, PlaybookSummary } from '../../types';
import { PlaybookNavRow } from './PlaybookNavRow';
import { ProjectCard } from './ProjectCard';
import { SessionSection } from './SessionSection';

const state = vi.hoisted(() => ({ blueprints: new Map<string, unknown>() }));
vi.mock('../../hooks/useAttention', () => ({ useAttention: () => ({ blueprints: state.blueprints }) }));
vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: true, toggle: () => {}, close: () => {} }),
}));
vi.mock('../../hooks/useSessionPin', () => ({ useSessionPin: () => vi.fn() }));

const item = (id: string): AttentionItem => ({ kind: 'blueprint', id, title: id, reason: 'needs input', since: '2026-01-01T00:00:00Z' });

const sess = (id: string, kind: 'chat' | 'architect'): ChatSession => ({
  id,
  title: `T ${id}`,
  kind,
  status: 'cold',
  created_at: '2026-10-01T10:00:00Z',
  last_active: '2026-10-01T10:00:00Z',
  created_by: 'human:alice',
});

const playbook: PlaybookSummary = {
  id: 'pb',
  title: 'Rollout',
  complete: 0,
  total: 1,
  segments: ['pending'],
  projects: 0,
  project_names: [],
  updated_at: '2026-10-01T10:00:00Z',
};

beforeEach(() => {
  state.blueprints = new Map();
});

describe('sidebar wait signals', () => {
  it('a blueprint row carries the badge exactly while its item is listed', () => {
    const ui = () => (
      <MemoryRouter>
        <SessionSection kind="architect" sessions={[sess('a', 'architect'), sess('b', 'architect')]} />
      </MemoryRouter>
    );
    const { rerender } = render(ui());
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);

    state.blueprints = new Map([['a', item('a')]]);
    rerender(ui());
    const badges = document.querySelectorAll('.wait-badge');
    expect(badges).toHaveLength(1);
    expect(screen.getByText('T a').closest('li')).toContainElement(badges[0] as HTMLElement);

    state.blueprints = new Map();
    rerender(ui());
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
  });

  it('a chat row never carries the badge', () => {
    state.blueprints = new Map([['c', item('c')]]);
    render(
      <MemoryRouter>
        <SessionSection kind="chat" sessions={[sess('c', 'chat')]} />
      </MemoryRouter>,
    );
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
  });

  it('a playbook row carries the badge only when waiting', () => {
    const { rerender } = render(
      <MemoryRouter>
        <PlaybookNavRow playbook={playbook} />
      </MemoryRouter>,
    );
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
    rerender(
      <MemoryRouter>
        <PlaybookNavRow playbook={playbook} waiting />
      </MemoryRouter>,
    );
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(1);
  });

  it('a project row shows the count while above zero', () => {
    const { rerender } = render(<ProjectCard name="p" isActive={false} waiting={3} />);
    expect(within(document.body).getByText('3')).toHaveClass('wait-badge');
    rerender(<ProjectCard name="p" isActive={false} waiting={0} />);
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
    rerender(<ProjectCard name="p" isActive={false} />);
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
  });

  it('a project row keeps the count beside the active-agents pill, not between the name and it', () => {
    const summary = { active_agents: [{}, {}], state_counts: {} } as unknown as DashboardData;
    render(<ProjectCard name="proj" isActive={false} waiting={3} summary={summary} />);
    const group = screen.getByText('3').parentElement;
    expect(group).toContainElement(screen.getByText('2'));
    expect(group).not.toContainElement(screen.getByText('proj'));
  });
});
