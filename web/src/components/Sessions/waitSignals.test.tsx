import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { AttentionItem, ChatSession, PlaybookSummary } from '../../types';
import { PlaybookTableRow } from '../Playbooks/PlaybookTableRow';
import { SessionRow } from './SessionRow';
import { SessionsTabs } from './SessionsTabs';
import { SessionsToolbar } from './SessionsToolbar';
import { EMPTY_FILTER, filterSessions } from './sessionFilter';

const item = (kind: AttentionItem['kind'], id: string, reason: string): AttentionItem => ({
  kind,
  id,
  title: id,
  reason,
  since: '2026-01-01T00:00:00Z',
});

const session = (id: string): ChatSession => ({
  id,
  title: `T ${id}`,
  kind: 'architect',
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

const noop = vi.fn();

describe('Sessions wait signals', () => {
  it('a blueprint row shows the badge and the reason of its item, then drops both', () => {
    const waiting = item('blueprint', 'a', 'Asked a question');
    const row = (w?: AttentionItem) => (
      <table>
        <tbody>
          <SessionRow session={session('a')} showMode ticked={false} onToggle={noop} onPin={noop} onOpen={noop} waiting={w} />
        </tbody>
      </table>
    );
    const { rerender } = render(row(waiting));
    const tr = screen.getByRole('row');
    expect(within(tr).getByText(waiting.reason)).toBeInTheDocument();
    expect(tr.querySelectorAll('.wait-badge')).toHaveLength(1);
    rerender(row());
    expect(screen.queryByText(waiting.reason)).toBeNull();
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
  });

  it('a playbook row shows the badge and the reason of its item', () => {
    const waiting = item('playbook', 'pb', 'Manual step pending');
    const row = (w?: AttentionItem) => (
      <table>
        <tbody>
          <PlaybookTableRow playbook={playbook} ticked={false} onToggle={noop} onPin={noop} onOpen={noop} waiting={w} />
        </tbody>
      </table>
    );
    const { rerender } = render(row(waiting));
    expect(screen.getByText(waiting.reason)).toBeInTheDocument();
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(1);
    rerender(row());
    expect(screen.queryByText(waiting.reason)).toBeNull();
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
  });

  it('badges exactly the tabs that hold a waiting item', () => {
    const counts = { chat: 1, architect: 2, playbooks: 3 };
    const { rerender } = render(<SessionsTabs active="chat" counts={counts} onChange={noop} waiting={new Set()} />);
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(0);
    rerender(<SessionsTabs active="chat" counts={counts} onChange={noop} waiting={new Set(['architect'])} />);
    expect(within(screen.getByRole('tab', { name: /Blueprints/ })).getByText('!')).toBeInTheDocument();
    expect(document.querySelectorAll('.wait-badge')).toHaveLength(1);
  });

  it('Status waiting filters to the waiting ids', () => {
    const list = [session('a'), session('b')];
    const f = { ...EMPTY_FILTER, status: 'waiting' as const };
    expect(filterSessions(list, f, new Set(['b'])).map((s) => s.id)).toEqual(['b']);
    expect(filterSessions(list, f)).toEqual([]);
  });

  it('offers Status waiting only when asked', () => {
    const { rerender } = render(<SessionsToolbar sessions={[]} filter={EMPTY_FILTER} onChange={noop} />);
    expect(screen.queryByRole('option', { name: 'waiting' })).toBeNull();
    rerender(<SessionsToolbar sessions={[]} filter={EMPTY_FILTER} onChange={noop} offerWaiting />);
    fireEvent.change(screen.getByLabelText(/Status/), { target: { value: 'waiting' } });
    expect(noop).toHaveBeenCalledWith({ ...EMPTY_FILTER, status: 'waiting' });
  });
});
