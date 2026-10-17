import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { refreshArtifact, resetArtifactCache, useBlueprintArtifact, loadArtifact } from './artifactCache';
import { DESIGN_V2 } from '../../test-utils/blueprintFixtures';

vi.mock('../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
const get = vi.mocked(api.getBlueprintArtifact);

describe('artifactCache', () => {
  beforeEach(() => {
    resetArtifactCache();
    get.mockReset();
  });

  it('fetches an artifact once for every reader', async () => {
    get.mockResolvedValue(DESIGN_V2);
    const a = renderHook(() => useBlueprintArtifact('S1', 'solution_design', 2));
    const b = renderHook(() => useBlueprintArtifact('S1', 'solution_design', 2));
    await waitFor(() => expect(a.result.current).toEqual({ status: 'ready', artifact: DESIGN_V2 }));
    expect(b.result.current).toEqual({ status: 'ready', artifact: DESIGN_V2 });
    expect(get).toHaveBeenCalledTimes(1);
  });

  it('reads nothing without a version', () => {
    const { result } = renderHook(() => useBlueprintArtifact('S1', 'solution_design', null));
    expect(result.current).toBeNull();
    expect(get).not.toHaveBeenCalled();
  });

  it('retries after an error', async () => {
    get.mockRejectedValueOnce({ error: 'down', code: 'BACKEND_UNAVAILABLE' }).mockResolvedValueOnce(DESIGN_V2);
    const { result } = renderHook(() => useBlueprintArtifact('S1', 'solution_design', 2));
    await waitFor(() => expect(result.current?.status).toBe('error'));
    act(() => loadArtifact('S1', 'solution_design', 2));
    await waitFor(() => expect(result.current?.status).toBe('ready'));
  });

  it('refreshes a stored artifact and keeps the old one shown meanwhile', async () => {
    get.mockResolvedValueOnce(DESIGN_V2);
    const { result } = renderHook(() => useBlueprintArtifact('S1', 'emission', 1));
    await waitFor(() => expect(result.current?.status).toBe('ready'));
    let finish: (v: typeof DESIGN_V2) => void = () => {};
    get.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    act(() => refreshArtifact('S1', 'emission', 1));
    expect(result.current).toEqual({ status: 'ready', artifact: DESIGN_V2 });
    const next = { ...DESIGN_V2, body: 'newer' };
    act(() => finish(next));
    await waitFor(() => expect(result.current).toEqual({ status: 'ready', artifact: next }));
  });

  it('ignores a refresh for an artifact nobody read', () => {
    refreshArtifact('S1', 'emission', 1);
    expect(get).not.toHaveBeenCalled();
  });
});
