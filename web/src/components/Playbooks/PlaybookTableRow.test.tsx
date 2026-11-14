import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { PlaybookTableRow } from './PlaybookTableRow';
import { runStatusChip } from './playbookUtils';
import type { PlaybookRunStatus, PlaybookSummary } from '../../types';

function pb(o: Partial<PlaybookSummary> = {}): PlaybookSummary {
  return {
    id: 'rollout',
    title: 'Rollout',
    complete: 1,
    total: 3,
    segments: ['complete', 'pending', 'pending'],
    projects: 2,
    project_names: ['alpha', 'beta'],
    next: { type: 'card', project: 'alpha', card: 'ALPHA-7', title: 'Wire the flag' },
    updated_at: '2026-10-01T10:00:00Z',
    ...o,
  };
}

function renderRow(playbook: PlaybookSummary) {
  const handlers = { onToggle: vi.fn(), onPin: vi.fn(), onOpen: vi.fn() };
  render(
    <table>
      <tbody>
        <PlaybookTableRow playbook={playbook} ticked={false} {...handlers} />
      </tbody>
    </table>,
  );
  return { row: screen.getByRole('row'), ...handlers };
}

describe('PlaybookTableRow', () => {
  it.each<PlaybookRunStatus | undefined>(['running', 'waiting', 'stopped', 'completed', undefined])(
    'with the run %s, names the next entry',
    (run_status) => {
      const { row } = renderRow(pb({ run_status, runnable: true }));
      expect(within(row).getByText('Wire the flag')).toBeInTheDocument();
      expect(within(row).getByText('ALPHA-7')).toBeInTheDocument();
    },
  );

  it('names no next entry once nothing is next', () => {
    const { row } = renderRow(pb({ run_status: 'running', next: undefined }));
    expect(within(row).queryByText('Wire the flag')).toBeNull();
    expect(within(row).queryByText('ALPHA-7')).toBeNull();
  });

  it('names a manual next-up step by its text', () => {
    const { row } = renderRow(pb({ run_status: 'waiting', next: { type: 'manual', title: 'Confirm the push landed' } }));
    expect(within(row).getByText('Confirm the push landed')).toBeInTheDocument();
  });

  it('shows no agent active or missing card pill while no segment says so', () => {
    const { row } = renderRow(pb());
    expect(within(row).queryByText('agent active')).toBeNull();
    expect(within(row).queryByText('missing card')).toBeNull();
  });

  it('shows agent active and missing card from the segments', () => {
    const { row } = renderRow(pb({ segments: ['complete', 'active', 'missing'] }));
    expect(within(row).getByText('agent active')).toBeInTheDocument();
    expect(within(row).getByText('missing card')).toBeInTheDocument();
  });

  it('shows the run pill for a run', () => {
    const { row } = renderRow(pb({ run_status: 'stopped', runnable: true }));
    expect(within(row).getByText(runStatusChip('stopped').label)).toBeInTheDocument();
  });

  it('shows no run pill without a run', () => {
    const { row } = renderRow(pb({ runnable: true }));
    for (const s of ['running', 'waiting', 'stopped', 'completed'] as const) {
      expect(within(row).queryByText(runStatusChip(s).label)).toBeNull();
    }
  });

  it('carries the Jira epic key of an imported playbook without linking out of the row', () => {
    const { row } = renderRow(
      pb({ source: { system: 'jira', external_id: 'PROJ-412', external_url: 'https://example.atlassian.net/browse/PROJ-412' } }),
    );
    expect(within(row).getByText('PROJ-412')).toBeInTheDocument();
    expect(within(row).queryByRole('link')).toBeNull();
  });

  it('lists the project names and the progress', () => {
    const { row } = renderRow(pb());
    expect(within(row).getByText('alpha, beta')).toBeInTheDocument();
    expect(within(row).getByRole('img', { name: '1 of 3 complete' })).toBeInTheDocument();
  });

  it('opens on a click, but the tick box and the pin do their own job', () => {
    const { row, onOpen, onToggle, onPin } = renderRow(pb({ pinned: true }));

    fireEvent.click(within(row).getByRole('checkbox'));
    expect(onToggle).toHaveBeenCalledWith('rollout');
    fireEvent.click(within(row).getByRole('button', { pressed: true }));
    expect(onPin).toHaveBeenCalledWith(expect.objectContaining({ id: 'rollout' }));
    expect(onOpen).not.toHaveBeenCalled();

    fireEvent.click(within(row).getByText('Rollout'));
    expect(onOpen).toHaveBeenCalledWith('rollout');
  });
});
