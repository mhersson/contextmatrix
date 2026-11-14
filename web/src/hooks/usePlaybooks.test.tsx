import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { PlaybooksProvider, usePlaybooks } from './usePlaybooks';
import { api } from '../api/client';
import type { BoardEvent, PlaybookSummary } from '../types';

const bus = vi.hoisted(() => ({
  handlers: new Map<string, (e: BoardEvent) => void>(),
  epoch: 0,
}));
vi.mock('./useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (pattern: string, fn: (e: BoardEvent) => void) => {
      bus.handlers.set(pattern, fn);
      return () => bus.handlers.delete(pattern);
    },
    connected: true,
    reconnectEpoch: bus.epoch,
  }),
}));

const emit = (type: string) => {
  const ev = { type } as BoardEvent;
  for (const [pattern, fn] of bus.handlers) {
    if (pattern === type || (pattern.endsWith('.*') && type.startsWith(pattern.slice(0, -1)))) fn(ev);
  }
};

const pb = (id: string): PlaybookSummary => ({ id, title: id, complete: 0, total: 1, segments: [], projects: 1, project_names: [], updated_at: '' });

function Probe() {
  const { playbooks, loaded, error } = usePlaybooks();
  return <div data-testid="out">{`${loaded}|${error ?? ''}|${playbooks.map((p) => p.id).join(',')}`}</div>;
}

const out = () => screen.getByTestId('out').textContent;
const mount = async () => {
  render(<PlaybooksProvider><Probe /></PlaybooksProvider>);
  await act(async () => {});
};

beforeEach(() => {
  vi.useFakeTimers();
  bus.handlers.clear();
  bus.epoch = 0;
  vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a')]);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('usePlaybooks', () => {
  it('fetches on mount', async () => {
    await mount();
    expect(out()).toBe('true||a');
  });

  it.each(['playbook.updated', 'card.created', 'card.updated', 'card.state_changed', 'card.deleted'])(
    'refetches once per burst on %s',
    async (type) => {
      await mount();
      vi.mocked(api.listPlaybooks).mockClear();
      act(() => { emit(type); emit(type); emit(type); });
      await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
      expect(api.listPlaybooks).toHaveBeenCalledTimes(1);
    },
  );

  it('ignores card events that cannot change a playbook', async () => {
    await mount();
    vi.mocked(api.listPlaybooks).mockClear();
    act(() => { emit('card.log_added'); });
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(api.listPlaybooks).not.toHaveBeenCalled();
  });

  it('keeps the last list on a failed refetch and reports the error', async () => {
    await mount();
    vi.mocked(api.listPlaybooks).mockRejectedValue(new Error('boom'));
    act(() => { emit('playbook.updated'); });
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(out()).toBe('true|boom|a');
  });

  it('reports the error and stays unloaded when the first fetch fails', async () => {
    vi.mocked(api.listPlaybooks).mockRejectedValue(new Error('boom'));
    await mount();
    expect(out()).toBe('false|boom|');
  });
});
