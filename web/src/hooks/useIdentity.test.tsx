import { renderHook } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useIdentity } from './useIdentity';

const mocks = vi.hoisted(() => ({
  useAuth: vi.fn(),
  useAgentId: vi.fn(),
}));

vi.mock('./useAuth', () => ({ useAuth: mocks.useAuth }));
vi.mock('./useAgentId', () => ({ useAgentId: mocks.useAgentId }));

beforeEach(() => vi.resetAllMocks());

describe('useIdentity', () => {
  it('multi + session → human:<username>, agent id disabled', () => {
    mocks.useAuth.mockReturnValue({ mode: 'multi', user: { username: 'alice', display_name: '', is_admin: false } });
    mocks.useAgentId.mockReturnValue({ agentId: null });

    const { result } = renderHook(() => useIdentity());

    expect(result.current.identity).toBe('human:alice');
    expect(mocks.useAgentId).toHaveBeenCalledWith(false);
  });

  it('multi + logged out → null', () => {
    mocks.useAuth.mockReturnValue({ mode: 'multi', user: null });
    mocks.useAgentId.mockReturnValue({ agentId: null });

    const { result } = renderHook(() => useIdentity());

    expect(result.current.identity).toBeNull();
  });

  it('none → localStorage agent id', () => {
    mocks.useAuth.mockReturnValue({ mode: 'none', user: null });
    mocks.useAgentId.mockReturnValue({ agentId: 'human:web-abcd1234' });

    const { result } = renderHook(() => useIdentity());

    expect(result.current.identity).toBe('human:web-abcd1234');
    expect(mocks.useAgentId).toHaveBeenCalledWith(true);
  });
});
