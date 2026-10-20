import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AboutComponent, AboutResponse } from '../../types';
import { AboutDialog } from './AboutDialog';

const state = vi.hoisted(() => ({
  about: null as AboutResponse | null,
  seen: {} as Record<string, { number: number; merged_at: string }>,
  refresh: vi.fn(),
  markSeen: vi.fn(),
}));

vi.mock('../../hooks/useAbout', () => ({ useAbout: () => state }));

const CM_SHA = 'c2c9459' + 'f'.repeat(33);
const A = 'A'.repeat(40);
const B = 'B'.repeat(40);

const m516 = '2026-10-20T12:00:00Z';
const m515 = '2026-10-19T12:00:00Z';
const m514 = '2026-10-18T12:00:00Z';

function fixture(): AboutResponse {
  return {
    components: [
      {
        name: 'contextmatrix',
        status: 'running',
        build: {
          version: '',
          commit: CM_SHA,
          build_time: '2026-10-20T12:14:00Z',
          changes: [
            { number: 516, kind: 'fix', title: 'Fix sign-off', merged_at: m516 },
            { number: 515, kind: 'fix', title: 'Polish prompt', merged_at: m515 },
            { number: 514, kind: 'feat', title: 'Add mockups', merged_at: m514 },
          ],
        },
      },
      {
        name: 'agent',
        status: 'connected',
        build: {
          commit: A,
          build_time: '2026-10-20T10:00:00Z',
          changes: [{ number: 9, kind: 'change', title: 'Show the popup', merged_at: '2026-10-20T09:00:00Z' }],
        },
        worker_image_revision: B,
      },
      { name: 'chat', status: 'connected' },
      { name: 'architect', status: 'unreachable' },
    ],
  };
}

function row(label: string) {
  return screen.getByRole('row', { name: new RegExp(`^${label}`) });
}

beforeEach(() => {
  state.about = fixture();
  state.seen = {};
  state.refresh = vi.fn().mockResolvedValue(null);
  state.markSeen = vi.fn();
});

async function openDialog(onClose = vi.fn()) {
  const utils = render(<AboutDialog open onClose={onClose} />);
  await act(async () => {});
  return { ...utils, onClose };
}

describe('AboutDialog', () => {
  it('lists every component in the running table', async () => {
    await openDialog();
    const rows = screen.getAllByRole('row').slice(1);
    const labels = ['ContextMatrix', 'Agent', 'Chat', 'Architect'];
    expect(rows).toHaveLength(4);
    rows.forEach((r, i) => expect(within(r).getAllByRole('cell')[0].textContent?.startsWith(labels[i])).toBe(true));
    expect(row('ContextMatrix')).toHaveTextContent('c2c9459');
    expect(row('ContextMatrix')).toHaveTextContent('running');
    expect(row('Chat')).toHaveTextContent('version unknown');
    expect(row('Chat')).toHaveTextContent('connected');
    expect(row('Architect')).toHaveTextContent('unreachable');
  });

  it('shows the worker image and the mismatch marker', async () => {
    const { unmount } = await openDialog();
    expect(row('Agent')).toHaveTextContent('image BBBBBBB');
    expect(row('Agent')).toHaveTextContent('differs');
    unmount();
    state.about!.components[1].worker_image_revision = A;
    await openDialog();
    expect(row('Agent')).toHaveTextContent('image AAAAAAA');
    expect(row('Agent')).not.toHaveTextContent('differs');
  });

  it('marks entries newer than the open-time snapshot', async () => {
    state.seen = { contextmatrix: { number: 515, merged_at: m515 } };
    await openDialog();
    const panel = screen.getByRole('tabpanel');
    expect(within(panel).getAllByText('NEW')).toHaveLength(1);
    expect(within(panel.querySelector('li')!).getByText('NEW')).toBeInTheDocument();
    expect(panel.querySelector('li')).toHaveTextContent('516');
    expect(screen.getByRole('tab', { name: /ContextMatrix/ })).toHaveTextContent('(new changes)');
    expect(screen.getByRole('tab', { name: /Agent/ })).toHaveTextContent('(new changes)');
    expect(screen.getByRole('tab', { name: /Chat/ })).not.toHaveTextContent('(new changes)');
    expect(screen.getByRole('tab', { name: /Architect/ })).not.toHaveTextContent('(new changes)');
  });

  it('snapshot is taken at open', async () => {
    state.seen = { contextmatrix: { number: 515, merged_at: m515 } };
    const { rerender } = await openDialog();
    state.seen = { contextmatrix: { number: 516, merged_at: m516 } };
    rerender(<AboutDialog open onClose={vi.fn()} />);
    expect(within(screen.getByRole('tabpanel')).getAllByText('NEW')).toHaveLength(1);
  });

  it('refreshes and marks the shown components seen on open', async () => {
    const fresh: AboutResponse = { components: [{ name: 'chat', status: 'connected' } as AboutComponent] };
    state.refresh.mockResolvedValue(fresh);
    const { unmount } = await openDialog();
    expect(state.refresh).toHaveBeenCalledTimes(1);
    expect(state.markSeen).toHaveBeenCalledTimes(1);
    expect(state.markSeen).toHaveBeenCalledWith(fresh.components);
    unmount();

    state.markSeen.mockClear();
    state.refresh.mockResolvedValue(null);
    await openDialog();
    expect(state.markSeen).toHaveBeenCalledTimes(1);
    expect(state.markSeen).toHaveBeenCalledWith(state.about!.components);
  });

  it('shows the empty states', async () => {
    state.about!.components.push({
      name: 'agent',
      status: 'connected',
      build: { commit: A, changes: [] },
    });
    await openDialog();
    fireEvent.click(screen.getByRole('tab', { name: /Chat/ }));
    expect(screen.getByText('Version unknown: this build does not report its changes.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: /Architect/ }));
    expect(screen.getByText('Architect is not reachable right now.')).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole('tab', { name: /Agent/ })[1]);
    expect(screen.getByText('No change history was recorded for this build.')).toBeInTheDocument();
  });

  it('renders kinds', async () => {
    await openDialog();
    const panel = screen.getByRole('tabpanel');
    expect(within(panel).getAllByText('fix')).toHaveLength(2);
    expect(within(panel).getByText('feat')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: /Agent/ }));
    expect(within(screen.getByRole('tabpanel')).getByText('change')).toBeInTheDocument();
  });

  it('closes on Escape and on the close button', async () => {
    const { onClose } = await openDialog();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('with no data shows the load error', async () => {
    state.about = null;
    await openDialog();
    expect(screen.getByText('Could not load what runs here. Try again later.')).toBeInTheDocument();
  });

  it('Escape closes the dialog without reaching page-level key handlers', async () => {
    const page = vi.fn();
    document.addEventListener('keydown', page);
    try {
      const { onClose } = await openDialog();
      fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
      expect(onClose).toHaveBeenCalledTimes(1);
      expect(page).not.toHaveBeenCalled();
    } finally {
      document.removeEventListener('keydown', page);
    }
  });

  it('keeps other keys pressed inside the dialog from the page shortcuts', async () => {
    const page = vi.fn();
    document.addEventListener('keydown', page);
    try {
      await openDialog();
      fireEvent.keyDown(screen.getByRole('dialog'), { key: 'n' });
      expect(page).not.toHaveBeenCalled();
    } finally {
      document.removeEventListener('keydown', page);
    }
  });
});
