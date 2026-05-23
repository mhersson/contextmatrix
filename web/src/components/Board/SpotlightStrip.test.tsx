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
