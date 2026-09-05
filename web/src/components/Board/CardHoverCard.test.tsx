import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { CardHoverCard } from './CardHoverCard';
import type { Card } from '../../types';

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Hover card',
  project: 'test',
  type: 'feature',
  state: 'todo',
  priority: 'high',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

function renderHover(card: Card) {
  return render(<CardHoverCard card={card} anchorRef={{ current: null }} id="hover-TEST-001" />);
}

/** Anchors the panel to a fake card rect inside a fake viewport. */
function renderAnchored(rect: Partial<DOMRect>, viewport: { width: number; height: number }, panelHeight = 100) {
  const anchor = document.createElement('div');
  anchor.getBoundingClientRect = () =>
    ({ top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}), ...rect }) as DOMRect;
  vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(viewport.width);
  vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(viewport.height);
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(panelHeight);
  render(<CardHoverCard card={baseCard} anchorRef={{ current: anchor }} id="hover-TEST-001" />);
  return screen.getByRole('tooltip');
}

describe('CardHoverCard', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('is a tooltip headed by id, type and priority', () => {
    renderHover(baseCard);
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveAttribute('id', 'hover-TEST-001');
    const head = within(tip).getByTestId('hover-head');
    expect(head).toHaveTextContent('TEST-001');
    expect(head).toHaveTextContent('feature');
    expect(head).toHaveTextContent('high');
  });

  it('lists every signal, including those the header cap hides', () => {
    renderHover({
      ...baseCard,
      depends_on: ['TEST-000'],
      dependencies_met: true,
      autonomous: true,
      mob_participants: 3,
      mob_phases: ['plan', 'review'],
      best_of_n: 2,
      worker_status: 'running',
      in_playbooks: ['rollout', 'cleanup'],
      labels: ['simple'],
    });
    const tip = screen.getByRole('tooltip');
    for (const label of [
      'All dependencies met',
      'Autonomous',
      'Mob session - 3 agents',
      'Best of 2 - candidates judged, best one adopted',
      'Worker running',
      'In playbooks: rollout, cleanup',
      'Simple - no decomposition',
    ]) {
      expect(within(tip).getByText(label)).toBeInTheDocument();
    }
  });

  it('names the blocking cards on the dependency row', () => {
    renderHover({
      ...baseCard,
      depends_on: ['TEST-000', 'TEST-002'],
      blocked_by: ['TEST-002'],
    });
    const tip = screen.getByRole('tooltip');
    expect(within(tip).getByText('Blocked by TEST-002')).toBeInTheDocument();
  });

  it('names the mob phases', () => {
    renderHover({ ...baseCard, mob_participants: 2, mob_phases: ['plan', 'review'] });
    expect(screen.getByText('plan, review')).toBeInTheDocument();
  });

  it('shows the HITL row when the card is not autonomous', () => {
    renderHover(baseCard);
    expect(screen.getByText('HITL - human in the loop')).toBeInTheDocument();
    expect(screen.queryByText('Autonomous')).not.toBeInTheDocument();
  });

  it('omits the HITL row when the card is autonomous', () => {
    renderHover({ ...baseCard, autonomous: true });
    expect(screen.queryByText('HITL - human in the loop')).not.toBeInTheDocument();
    expect(screen.getByText('Autonomous')).toBeInTheDocument();
  });

  it('renders each label as its own pill', () => {
    renderHover({ ...baseCard, labels: ['backend', 'needs-design'] });
    const row = screen.getByTestId('hover-labels');
    expect(within(row).getByText('backend')).toBeInTheDocument();
    expect(within(row).getByText('needs-design')).toBeInTheDocument();
  });

  it('omits the labels row when the card has none', () => {
    renderHover(baseCard);
    expect(screen.queryByTestId('hover-labels')).not.toBeInTheDocument();
  });

  it('floats to the right of the anchor when it fits', () => {
    const tip = renderAnchored({ top: 100, left: 300, right: 560 }, { width: 1600, height: 900 });
    expect(tip.style.left).toBe('568px');
    expect(tip.style.top).toBe('100px');
  });

  it('flips to the left of the anchor at the viewport edge', () => {
    const tip = renderAnchored({ top: 100, left: 1300, right: 1560 }, { width: 1600, height: 900 });
    // left - gap - width: 1300 - 8 - 240
    expect(tip.style.left).toBe('1052px');
  });

  it('clamps so the panel bottom stays inside the viewport', () => {
    const tip = renderAnchored({ top: 850, left: 300, right: 560 }, { width: 1600, height: 900 }, 100);
    // innerHeight - height - gap: 900 - 100 - 8
    expect(tip.style.top).toBe('792px');
  });
});
