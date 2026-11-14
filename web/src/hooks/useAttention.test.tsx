import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import { api } from '../api/client';
import type { AttentionItem } from '../types';
import { tabTitle } from '../lib/attention';
import { AttentionProvider, useAttention } from './useAttention';

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>();
  return { ...actual, api: { getAttention: vi.fn() } };
});

const bus = vi.hoisted(() => ({
  handlers: new Set<() => void>(),
  epoch: 0,
}));
vi.mock('./useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (_p: string, h: () => void) => {
      bus.handlers.add(h);
      return () => bus.handlers.delete(h);
    },
    connected: true,
    reconnectEpoch: bus.epoch,
  }),
}));

const get = vi.mocked(api.getAttention);

function item(id: string, over: Partial<AttentionItem> = {}): AttentionItem {
  return { kind: 'blueprint', id, title: id, reason: 'r', since: '2026-01-01T00:00:00Z', mine: true, ...over };
}

function Probe() {
  const a = useAttention();
  return (
    <div>
      <span data-testid="ids">{a.items.map((i) => i.id).join(',')}</span>
      <span data-testid="loaded">{String(a.loaded)}</span>
      <span data-testid="bp">{[...a.blueprints.keys()].join(',')}</span>
      <span data-testid="proj">{JSON.stringify(Object.fromEntries(a.projectCounts))}</span>
    </div>
  );
}

const renderIt = () => render(<AttentionProvider><Probe /></AttentionProvider>);
const emit = () => act(() => { bus.handlers.forEach((h) => h()); });

beforeEach(() => {
  get.mockReset();
  bus.handlers.clear();
  bus.epoch = 0;
  document.title = 'ContextMatrix';
});

describe('AttentionProvider', () => {
  it('fetches on mount and on attention.changed, and sets the tab title from the count', async () => {
    get.mockResolvedValueOnce([item('a'), item('b', { kind: 'card', project: 'p' })]);
    renderIt();
    await waitFor(() => expect(screen.getByTestId('ids').textContent).toBe('a,b'));
    // The title effect is passive: it can trail the commit that shows the list.
    await waitFor(() => expect(document.title).toBe(tabTitle(2)));
    expect(screen.getByTestId('bp').textContent).toBe('a');
    expect(screen.getByTestId('proj').textContent).toBe('{"p":1}');

    get.mockResolvedValueOnce([]);
    emit();
    await waitFor(() => expect(document.title).toBe(tabTitle(0)));
    expect(get).toHaveBeenCalledTimes(2);
  });

  it('leaves the plain tab title behind when it unmounts', async () => {
    get.mockResolvedValueOnce([item('a'), item('b')]);
    const { unmount } = renderIt();
    await waitFor(() => expect(document.title).toBe(tabTitle(2)));
    unmount();
    expect(document.title).toBe(tabTitle(0));
  });

  it('refetches on reconnect and on nothing else', async () => {
    get.mockResolvedValue([]);
    const { rerender } = renderIt();
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    rerender(<AttentionProvider><Probe /></AttentionProvider>);
    expect(get).toHaveBeenCalledTimes(1);
    bus.epoch = 1;
    rerender(<AttentionProvider><Probe /></AttentionProvider>);
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
  });

  it('keeps the last list when a refetch fails', async () => {
    get.mockResolvedValueOnce([item('a')]);
    renderIt();
    await waitFor(() => expect(screen.getByTestId('ids').textContent).toBe('a'));
    get.mockRejectedValueOnce(new Error('down'));
    emit();
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    expect(screen.getByTestId('ids').textContent).toBe('a');
    expect(document.title).toBe(tabTitle(1));
  });

  it('never lets a slower older answer overwrite a newer one', async () => {
    let resolveOld!: (v: AttentionItem[]) => void;
    get.mockReturnValueOnce(new Promise((r) => { resolveOld = r; }));
    renderIt();
    get.mockResolvedValueOnce([item('new')]);
    emit();
    await waitFor(() => expect(screen.getByTestId('ids').textContent).toBe('new'));
    await act(async () => { resolveOld([item('old')]); });
    expect(screen.getByTestId('ids').textContent).toBe('new');
  });
});
