import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CardSignalIcons } from './CardSignalIcons';
import type { Card } from '../../types';

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Signal card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

describe('CardSignalIcons', () => {
  it('renders nothing when the card has no signals', () => {
    const { container } = render(<CardSignalIcons card={baseCard} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows the autonomous icon', () => {
    render(<CardSignalIcons card={{ ...baseCard, autonomous: true }} />);
    expect(screen.getByRole('img', { name: 'Autonomous' })).toBeInTheDocument();
  });

  it('shows the mob icon with the participant count in the tooltip only', () => {
    render(<CardSignalIcons card={{ ...baseCard, mob_participants: 3 }} />);
    const icon = screen.getByRole('img', { name: 'Mob session - 3 agents' });
    expect(icon).toBeInTheDocument();
    expect(icon).not.toHaveTextContent('3');
  });

  it('hides the mob icon when mob is off', () => {
    render(<CardSignalIcons card={{ ...baseCard, mob_participants: 0 }} />);
    expect(screen.queryByRole('img', { name: /Mob session/ })).not.toBeInTheDocument();
  });

  it('maps each worker status to its own icon', () => {
    const { rerender } = render(<CardSignalIcons card={{ ...baseCard, worker_status: 'running' }} />);
    expect(screen.getByRole('img', { name: 'Worker running' })).toBeInTheDocument();

    rerender(<CardSignalIcons card={{ ...baseCard, worker_status: 'failed' }} />);
    expect(screen.getByRole('img', { name: 'Worker failed - open card for the log' })).toBeInTheDocument();

    rerender(<CardSignalIcons card={{ ...baseCard, worker_status: 'queued' }} />);
    expect(screen.getByRole('img', { name: 'Worker queued' })).toBeInTheDocument();

    rerender(<CardSignalIcons card={{ ...baseCard, worker_status: 'killed' }} />);
    expect(screen.getByRole('img', { name: 'Worker killed' })).toBeInTheDocument();
  });

  it('shows the playbook icon naming the playbooks', () => {
    const { rerender } = render(<CardSignalIcons card={{ ...baseCard, in_playbooks: ['rollout'] }} />);
    expect(screen.getByRole('img', { name: 'In playbook: rollout' })).toBeInTheDocument();

    rerender(<CardSignalIcons card={{ ...baseCard, in_playbooks: ['rollout', 'cleanup'] }} />);
    expect(screen.getByRole('img', { name: 'In playbooks: rollout, cleanup' })).toBeInTheDocument();
  });

  it('shows the dependency icon colored and labeled by state', () => {
    const { rerender } = render(
      <CardSignalIcons card={{ ...baseCard, depends_on: ['TEST-009'], dependencies_met: true }} />,
    );
    expect(screen.getByRole('img', { name: 'All dependencies met' })).toBeInTheDocument();

    // Go omitempty drops false, so the field may be absent entirely.
    rerender(<CardSignalIcons card={{ ...baseCard, depends_on: ['TEST-009'] }} />);
    expect(screen.getByRole('img', { name: 'Blocked by dependencies' })).toBeInTheDocument();
  });

  it('hides the dependency icon without depends_on', () => {
    render(<CardSignalIcons card={baseCard} />);
    expect(screen.queryByRole('img', { name: /dependencies/ })).not.toBeInTheDocument();
  });

  it('shows the Best-of-N trophy with the count in the tooltip', () => {
    render(<CardSignalIcons card={{ ...baseCard, best_of_n: 3 }} />);
    expect(
      screen.getByRole('img', { name: 'Best of 3 - candidates judged, best one adopted' }),
    ).toBeInTheDocument();
  });

  it('suppresses the Best-of-N trophy when mob execute is active', () => {
    render(
      <CardSignalIcons
        card={{ ...baseCard, best_of_n: 3, mob_participants: 3, mob_phases: ['plan', 'execute'] }}
      />,
    );
    expect(screen.queryByRole('img', { name: /Best of/ })).not.toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Mob session - 3 agents' })).toBeInTheDocument();
  });

  it('keeps the Best-of-N trophy when the mob skips execute', () => {
    render(
      <CardSignalIcons
        card={{ ...baseCard, best_of_n: 3, mob_participants: 3, mob_phases: ['plan', 'review'] }}
      />,
    );
    expect(
      screen.getByRole('img', { name: 'Best of 3 - candidates judged, best one adopted' }),
    ).toBeInTheDocument();
  });

  it('renders the special "simple" label as the feather icon', () => {
    render(<CardSignalIcons card={{ ...baseCard, labels: ['simple', 'other'] }} />);
    expect(screen.getByRole('img', { name: 'Simple - no decomposition' })).toBeInTheDocument();
    expect(screen.queryByText('simple')).not.toBeInTheDocument();
    expect(screen.queryByText('other')).not.toBeInTheDocument();
  });

  describe('header overflow cap', () => {
    // 7 signals: deps, auto, mob, best-of-n, worker, playbook, simple.
    const maxed = {
      ...baseCard,
      depends_on: ['TEST-009'],
      dependencies_met: true,
      autonomous: true,
      mob_participants: 3,
      mob_phases: ['plan'],
      best_of_n: 3,
      worker_status: 'running' as const,
      in_playbooks: ['rollout'],
      labels: ['simple'],
    };

    it('shows at most four icons plus an overflow chip naming the rest', () => {
      render(<CardSignalIcons card={maxed} />);
      // The four most important survive: deps, auto, mob, worker.
      expect(screen.getByRole('img', { name: 'All dependencies met' })).toBeInTheDocument();
      expect(screen.getByRole('img', { name: 'Autonomous' })).toBeInTheDocument();
      expect(screen.getByRole('img', { name: 'Mob session - 3 agents' })).toBeInTheDocument();
      expect(screen.getByRole('img', { name: 'Worker running' })).toBeInTheDocument();
      // The least important hide behind the chip (exact names: the chip's own
      // aria-label also contains these labels).
      expect(
        screen.queryByRole('img', { name: 'Best of 3 - candidates judged, best one adopted' }),
      ).not.toBeInTheDocument();
      expect(screen.queryByRole('img', { name: 'In playbook: rollout' })).not.toBeInTheDocument();
      expect(screen.queryByRole('img', { name: 'Simple - no decomposition' })).not.toBeInTheDocument();
      const chip = screen.getByText('+3');
      expect(chip).toHaveAttribute(
        'aria-label',
        '3 more signals: Best of 3 - candidates judged, best one adopted, In playbook: rollout, Simple - no decomposition',
      );
    });

    it('renders all icons without a chip at exactly four signals', () => {
      render(
        <CardSignalIcons
          card={{ ...baseCard, autonomous: true, mob_participants: 3, worker_status: 'running', in_playbooks: ['rollout'] }}
        />,
      );
      expect(screen.getByRole('img', { name: 'In playbook: rollout' })).toBeInTheDocument();
      expect(screen.queryByText(/^\+\d/)).not.toBeInTheDocument();
    });

    it('a failed worker always survives the cap', () => {
      render(<CardSignalIcons card={{ ...maxed, worker_status: 'failed' }} />);
      expect(
        screen.getByRole('img', { name: 'Worker failed - open card for the log' }),
      ).toBeInTheDocument();
    });
  });
});
