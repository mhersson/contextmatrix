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

  it('surfaces cards with unmet dependencies', () => {
    const cards = [
      mkCard({ id: 'CTX-3', title: 'blocked', depends_on: ['CTX-1'], dependencies_met: false }),
    ];
    render(<SpotlightStrip cards={cards} onCardClick={() => {}} />);
    expect(screen.getByText('blocked')).toBeInTheDocument();
  });

  it('renders nothing when there are no surfaced cards', () => {
    const { container } = render(<SpotlightStrip cards={[mkCard({})]} onCardClick={() => {}} />);
    expect(container.firstChild).toBeNull();
  });

  it('fires onCardClick when a spotlight card is clicked', () => {
    const handler = vi.fn();
    render(<SpotlightStrip cards={[mkCard({ id: 'CTX-9', state: 'stalled', title: 't' })]} onCardClick={handler} />);
    fireEvent.click(screen.getByRole('button', { name: /CTX-9/ }));
    expect(handler).toHaveBeenCalledWith('CTX-9');
  });
});
