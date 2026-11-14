import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import type { AttentionItem } from '../../types';
import { attentionHref } from '../../lib/attention';
import { WaitingSection } from './WaitingSection';

const state = vi.hoisted(() => ({ items: [] as AttentionItem[], close: vi.fn() }));
vi.mock('../../hooks/useAttention', () => ({ useAttention: () => ({ items: state.items }) }));
vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: true, toggle: () => {}, close: state.close }),
}));

function Where() {
  const l = useLocation();
  return <span data-testid="where">{l.pathname + l.search}</span>;
}

const mk = (o: Partial<AttentionItem> & Pick<AttentionItem, 'kind' | 'id' | 'title'>): AttentionItem => ({
  reason: 'why',
  since: '2026-01-01T00:00:00Z',
  ...o,
});

beforeEach(() => {
  state.items = [];
  state.close.mockReset();
});

const renderIt = () =>
  render(
    <MemoryRouter>
      <WaitingSection />
      <Where />
    </MemoryRouter>,
  );

describe('WaitingSection', () => {
  it('is absent with no items', () => {
    renderIt();
    expect(screen.queryByRole('region')).toBeNull();
  });

  it('lists items in order and marks shared ones', () => {
    state.items = [
      mk({ kind: 'blueprint', id: 's1', title: 'First', mine: true }),
      mk({ kind: 'card', id: 'AB-1', project: 'p', title: 'Second' }),
    ];
    renderIt();
    const rows = within(screen.getByRole('region')).getAllByRole('button');
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('First');
    expect(rows[0].textContent).not.toMatch(/shared/);
    expect(rows[1].textContent).toContain('Second');
    expect(rows[1].textContent).toMatch(/shared/);
  });

  it.each(['card', 'sentinel'] as const)('leads a %s row with its card id', (kind) => {
    state.items = [mk({ kind, id: 'AB-1', project: 'p', title: 'T' })];
    renderIt();
    expect(within(screen.getByRole('region')).getByRole('button').textContent).toMatch(/^AB-1/);
  });

  it('names the playbook a card item holds', () => {
    state.items = [mk({ kind: 'card', id: 'AB-1', project: 'p', title: 'T', playbook: { id: 'pb', title: 'Release run' } })];
    renderIt();
    expect(screen.getByRole('region').textContent).toContain('Release run');
  });

  it.each([
    mk({ kind: 'blueprint', id: 's1', title: 'bp', mine: true }),
    mk({ kind: 'playbook', id: 'pb', title: 'pb', mine: true }),
    mk({ kind: 'card', id: 'AB-1', project: 'p', title: 'card' }),
    mk({ kind: 'sentinel', id: 'AB-2', project: 'p', title: 'sent' }),
  ])('opens its target and closes the drawer: %#', (item) => {
    state.items = [item];
    renderIt();
    fireEvent.click(within(screen.getByRole('region')).getByRole('button'));
    expect(screen.getByTestId('where').textContent).toBe(attentionHref(item));
    expect(state.close).toHaveBeenCalled();
  });
});
