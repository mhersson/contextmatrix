import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { api } from '../api/client';
import { useCardActions } from './useCardActions';
import type { Card } from '../types';

let patchCardMock = vi.spyOn(api, 'patchCard');

beforeEach(() => {
  patchCardMock = vi.spyOn(api, 'patchCard');
});

afterEach(() => {
  vi.restoreAllMocks();
});

const selectedCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
  depends_on: ['TEST-002'],
  dependencies_met: false,
};

/**
 * Simulates a real PATCH response where the server's `omitempty` tags drop a
 * cleared field's key entirely - unlike `{ ...card, field: undefined }`,
 * which still leaves an own `field` key (with value `undefined`) that a
 * later `{ ...defaults, ...updated }` spread would pick up over the default.
 */
function withoutFields<K extends keyof Card>(card: Card, ...keys: K[]): Card {
  const copy: Card = { ...card };
  for (const k of keys) delete copy[k];
  return copy;
}

function setup(overrides: Partial<Parameters<typeof useCardActions>[0]> = {}) {
  const updateCardLocally = vi.fn();
  const { result } = renderHook(() =>
    useCardActions({
      selectedProject: 'test',
      selectedCard,
      cards: [selectedCard],
      updateCardLocally,
      removeCardLocally: vi.fn(),
      suppressSSE: vi.fn(),
      unsuppressSSE: vi.fn(),
      showToast: vi.fn(),
      onCardDeleted: vi.fn(),
      ...overrides,
    }),
  );
  return { result, updateCardLocally };
}

describe('useCardActions - handleCardSave clear-safe merge', () => {
  it('normalizes depends_on to [] and dependencies_met to undefined when the PATCH response omits them (cleared)', async () => {
    // Server omits both omitempty fields when the last dependency is removed.
    patchCardMock.mockResolvedValueOnce(withoutFields(selectedCard, 'depends_on', 'dependencies_met'));
    const { result, updateCardLocally } = setup();

    await act(async () => {
      await result.current.handleCardSave({ depends_on: [] });
    });

    expect(updateCardLocally).toHaveBeenCalledOnce();
    const [cardId, updates] = updateCardLocally.mock.calls[0];
    expect(cardId).toBe('TEST-001');
    expect(updates).toMatchObject({ depends_on: [], dependencies_met: undefined });
    expect('dependencies_met' in updates).toBe(true);
  });

  it('drops a stale blocked_by when the PATCH response omits it (all deps met)', async () => {
    patchCardMock.mockResolvedValueOnce(
      withoutFields({ ...selectedCard, dependencies_met: true, blocked_by: ['TEST-002'] }, 'blocked_by'),
    );
    const { result, updateCardLocally } = setup();

    await act(async () => {
      await result.current.handleCardSave({ depends_on: ['TEST-002'] });
    });

    const [, updates] = updateCardLocally.mock.calls[0];
    expect(updates).toMatchObject({ dependencies_met: true, blocked_by: undefined });
    expect('blocked_by' in updates).toBe(true);
  });

  it('passes through a populated depends_on and a true dependencies_met unchanged', async () => {
    patchCardMock.mockResolvedValueOnce({
      ...selectedCard,
      depends_on: ['TEST-003'],
      dependencies_met: true,
    });
    const { result, updateCardLocally } = setup();

    await act(async () => {
      await result.current.handleCardSave({ depends_on: ['TEST-003'] });
    });

    const [, updates] = updateCardLocally.mock.calls[0];
    expect(updates).toMatchObject({ depends_on: ['TEST-003'], dependencies_met: true });
  });

  it('normalizes labels to [] when the PATCH response omits them (cleared)', async () => {
    // selectedCard never sets labels, so this response already omits the key -
    // the same shape a real clear-to-empty PATCH response would have.
    patchCardMock.mockResolvedValueOnce(withoutFields(selectedCard, 'labels'));
    const { result, updateCardLocally } = setup();

    await act(async () => {
      await result.current.handleCardSave({ labels: [] });
    });

    const [, updates] = updateCardLocally.mock.calls[0];
    expect(updates).toMatchObject({ labels: [] });
  });

  it('clears assigned_agent and last_heartbeat when a terminal-state PATCH response omits them', async () => {
    // A save to not_planned/done releases the claim server-side; both fields
    // are omitempty, so the response drops the keys and the merge must clear
    // them or the panel keeps a stale claim (Delete stays disabled).
    const claimed: Card = {
      ...selectedCard,
      assigned_agent: 'human:alice',
      last_heartbeat: '2026-01-01T00:00:00Z',
    };
    patchCardMock.mockResolvedValueOnce(
      withoutFields({ ...claimed, state: 'not_planned' }, 'assigned_agent', 'last_heartbeat'),
    );
    const { result, updateCardLocally } = setup({ selectedCard: claimed, cards: [claimed] });

    await act(async () => {
      await result.current.handleCardSave({ state: 'not_planned' });
    });

    const [, updates] = updateCardLocally.mock.calls[0];
    expect(updates).toMatchObject({ state: 'not_planned', assigned_agent: '' });
    expect('last_heartbeat' in updates).toBe(true);
    expect(updates.last_heartbeat).toBeUndefined();
  });

  it('preserves a GET-only hydrated field (subtask_cost_usd) the PATCH response omits', async () => {
    // subtask_cost_usd is computed on read, never part of a PATCH round trip -
    // it must not be reset to a default just because this response omits it.
    patchCardMock.mockResolvedValueOnce(withoutFields(selectedCard, 'subtask_cost_usd'));
    const { result, updateCardLocally } = setup();

    await act(async () => {
      await result.current.handleCardSave({ title: 'Renamed' });
    });

    const [, updates] = updateCardLocally.mock.calls[0];
    expect('subtask_cost_usd' in updates).toBe(false);
  });
});
