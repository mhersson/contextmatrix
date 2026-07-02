import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';

const getModelCatalog = vi.fn();
vi.mock('../api/client', () => ({ api: { getModelCatalog: (...a: unknown[]) => getModelCatalog(...a) } }));

describe('useModelCatalog', () => {
  beforeEach(() => {
    vi.resetModules();
    getModelCatalog.mockReset();
  });

  it('fetches and returns the catalog when enabled', async () => {
    getModelCatalog.mockResolvedValue({
      source: 'openrouter',
      models: [{ id: 'anthropic/claude-sonnet-4.5', max_tokens: 200000 }],
    });
    const { useModelCatalog } = await import('./useModelCatalog');
    const { result } = renderHook(() => useModelCatalog(true));
    await waitFor(() =>
      expect(result.current.models.map((m) => m.id)).toEqual(['anthropic/claude-sonnet-4.5']),
    );
    expect(result.current.source).toBe('openrouter');
  });

  it('stays empty when disabled', async () => {
    const { useModelCatalog } = await import('./useModelCatalog');
    const { result } = renderHook(() => useModelCatalog(false));
    expect(result.current.models).toEqual([]);
    expect(getModelCatalog).not.toHaveBeenCalled();
  });

  it('returns the empty catalog on fetch failure', async () => {
    getModelCatalog.mockRejectedValue(new Error('boom'));
    const { useModelCatalog } = await import('./useModelCatalog');
    const { result } = renderHook(() => useModelCatalog(true));
    await waitFor(() => expect(getModelCatalog).toHaveBeenCalled());
    expect(result.current.models).toEqual([]);
  });
});
