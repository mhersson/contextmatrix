import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SpotlightStrip } from './SpotlightStrip';
import type { Card } from '../../types';

function mkCard(over: Partial<Card>): Card {
  return {
    id: 'CTX-1', title: '', project: 'p', type: 'task', state: 'todo',
    priority: 'medium', created: '2026-05-17T00:00:00Z', updated: '2026-05-17T00:00:00Z',
    body: '', ...over,
  };
}

describe('SpotlightStrip', () => {
  it('surfaces stalled cards', () => {
    const cards = [
      mkCard({ id: 'CTX-1', title: 'normal' }),
      mkCard({ id: 'CTX-2', title: 'stuck', state: 'stalled' }),
    ];
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} />);
    expect(screen.getByText('stuck')).toBeInTheDocument();
    expect(screen.queryByText('normal')).not.toBeInTheDocument();
  });

  it('does NOT surface dep-blocked cards in non-blocked states', () => {
    const cards = [
      mkCard({ id: 'CTX-3', title: 'dep-blocked', state: 'todo', depends_on: ['CTX-1'], dependencies_met: false }),
    ];
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} />);
    expect(screen.queryByText('dep-blocked')).not.toBeInTheDocument();
  });

  it("surfaces cards in 'blocked' state", () => {
    const cards = [
      mkCard({ id: 'CTX-4', title: 'in-blocked-state', state: 'blocked' }),
    ];
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} />);
    expect(screen.getByText('in-blocked-state')).toBeInTheDocument();
  });

  it('surfaces stalled cards even with unmet dependencies', () => {
    const cards = [
      mkCard({ id: 'CTX-5', title: 'stalled-with-deps', state: 'stalled', depends_on: ['CTX-1'], dependencies_met: false }),
    ];
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} />);
    expect(screen.getByText('stalled-with-deps')).toBeInTheDocument();
  });

  it('aria-label includes the card state for stalled and blocked cards', () => {
    const cards = [
      mkCard({ id: 'CTX-6', title: 'stalled-card', state: 'stalled' }),
      mkCard({ id: 'CTX-7', title: 'blocked-card', state: 'blocked' }),
    ];
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} />);
    expect(screen.getByRole('button', { name: 'Open CTX-6 – stalled' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open CTX-7 – blocked' })).toBeInTheDocument();
  });

  it('renders an "all clear" placeholder when there are no surfaced cards', () => {
    render(<SpotlightStrip cards={[mkCard({})]} onCardClick={() => {}} />);
    expect(screen.getByText('Needs Attention')).toBeInTheDocument();
    expect(screen.getByText(/all clear/i)).toBeInTheDocument();
    expect(screen.getByText(/no stalled or blocked cards/i)).toBeInTheDocument();
  });

  it('fires onCardClick when a spotlight card is clicked', () => {
    const handler = vi.fn();
    render(<SpotlightStrip cards={[mkCard({ id: 'CTX-9', state: 'stalled', title: 't' })]} onCardClick={handler} />);
    fireEvent.click(screen.getByRole('button', { name: /CTX-9/ }));
    expect(handler).toHaveBeenCalledWith('CTX-9');
  });
});

describe('SpotlightStrip - subtask phase strip & peek', () => {
  const parent = mkCard({ id: 'CTX-10', title: 'stalled parent', state: 'stalled' });
  const subA = mkCard({ id: 'CTX-11', parent: 'CTX-10', type: 'subtask', state: 'in_progress', title: 'Sub A' });
  const subB = mkCard({ id: 'CTX-12', parent: 'CTX-10', type: 'subtask', state: 'done', title: 'Sub B' });
  const cards = [parent, subA, subB];
  const subtasksByParent = new Map([['CTX-10', [subA, subB]]]);

  it('renders the phase strip for a stalled parent with subtasks', () => {
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} subtasksByParent={subtasksByParent} />);
    expect(screen.getByRole('button', { name: '2 subtasks' })).toBeInTheDocument();
  });

  it('renders no strip when the surfaced card has no subtasks', () => {
    render(<SpotlightStrip cards={[parent]} onCardClick={() => {}} subtasksByParent={new Map()} />);
    expect(screen.queryByRole('button', { name: /subtask/ })).not.toBeInTheDocument();
  });

  it('strip click toggles the peek list without opening the parent', () => {
    const handler = vi.fn();
    render(<SpotlightStrip cards={cards} onCardClick={handler} subtasksByParent={subtasksByParent} />);
    const strip = screen.getByRole('button', { name: '2 subtasks' });
    fireEvent.click(strip);
    expect(strip).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByTitle('Sub A')).toBeInTheDocument();
    expect(handler).not.toHaveBeenCalled();
    fireEvent.click(strip);
    expect(strip).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTitle('Sub A')).not.toBeInTheDocument();
  });

  it('peek row click calls onCardClick once with the subtask id', () => {
    const handler = vi.fn();
    render(<SpotlightStrip cards={cards} onCardClick={handler} subtasksByParent={subtasksByParent} />);
    fireEvent.click(screen.getByRole('button', { name: '2 subtasks' }));
    fireEvent.click(screen.getByTitle('Sub A'));
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith('CTX-11');
  });

  it('clicking the card outside the strip still opens the parent', () => {
    const handler = vi.fn();
    render(<SpotlightStrip cards={cards} onCardClick={handler} subtasksByParent={subtasksByParent} />);
    fireEvent.click(screen.getByRole('button', { name: 'Open CTX-10 – stalled' }));
    expect(handler).toHaveBeenCalledWith('CTX-10');
  });

  it('the open control is a native button, not a role="button" container', () => {
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} subtasksByParent={subtasksByParent} />);
    const open = screen.getByRole('button', { name: 'Open CTX-10 – stalled' });
    expect(open.tagName).toBe('BUTTON');
    // The strip button must not be an ARIA descendant of another button.
    const strip = screen.getByRole('button', { name: '2 subtasks' });
    expect(open.contains(strip)).toBe(false);
  });

  it('a flash targeting a subtask holds the peek open and flashes the card', () => {
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
    const { container } = render(
      <SpotlightStrip
        cards={cards}
        onCardClick={() => {}}
        subtasksByParent={subtasksByParent}
        flashCardId="CTX-11"
      />
    );
    expect(screen.getByRole('button', { name: '2 subtasks' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByTitle('Sub A')).toBeInTheDocument();
    expect(container.querySelector('.spotlight-card')?.className).toContain('animate-card-flash');
  });

  it('a blocked parent with subtasks gets the same strip', () => {
    const blocked = mkCard({ id: 'CTX-20', title: 'blocked parent', state: 'blocked' });
    const sub = mkCard({ id: 'CTX-21', parent: 'CTX-20', type: 'subtask', title: 'Blocked sub' });
    render(
      <SpotlightStrip
        cards={[blocked, sub]}
        onCardClick={() => {}}
        subtasksByParent={new Map([['CTX-20', [sub]]])}
      />
    );
    expect(screen.getByRole('button', { name: '1 subtask' })).toBeInTheDocument();
  });

  it('peek state is per card', () => {
    const other = mkCard({ id: 'CTX-30', title: 'other stalled', state: 'stalled' });
    const otherSub = mkCard({ id: 'CTX-31', parent: 'CTX-30', type: 'subtask', title: 'Other sub' });
    const map = new Map([
      ['CTX-10', [subA, subB]],
      ['CTX-30', [otherSub]],
    ]);
    render(<SpotlightStrip cards={[...cards, other, otherSub]} onCardClick={() => {}} subtasksByParent={map} />);
    fireEvent.click(screen.getByRole('button', { name: '2 subtasks' }));
    expect(screen.getByRole('button', { name: '2 subtasks' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('button', { name: '1 subtask' })).toHaveAttribute('aria-expanded', 'false');
  });
});
