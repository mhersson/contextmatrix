import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useAgentId } from './useAgentId';

const STORAGE_KEY = 'contextmatrix-agent-id';
const ID_PATTERN = /^human:web-[a-f0-9]{8}$/;

// Mock localStorage with a real backing store + spy-able methods so individual
// tests can swap implementations to simulate Safari Private Browsing / quota
// exhaustion / disabled storage.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      store = {};
    }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock });

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

describe('useAgentId', () => {
  it('generates a fresh id and persists it when storage is empty', () => {
    const { result } = renderHook(() => useAgentId());
    expect(result.current.agentId).toMatch(ID_PATTERN);
    expect(localStorageMock.getItem(STORAGE_KEY)).toBe(result.current.agentId);
  });

  it('preserves an existing localStorage id across mounts', () => {
    localStorageMock.setItem(STORAGE_KEY, 'human:web-abcdef12');
    const { result } = renderHook(() => useAgentId());
    expect(result.current.agentId).toBe('human:web-abcdef12');
  });
});

describe('useAgentId localStorage resilience', () => {
  it('falls back to in-memory id when localStorage.getItem throws', () => {
    localStorageMock.getItem.mockImplementationOnce(() => {
      throw new Error('quota');
    });
    const { result } = renderHook(() => useAgentId());
    expect(result.current.agentId).toMatch(ID_PATTERN);
  });

  it('falls back to in-memory id when localStorage.setItem throws', () => {
    localStorageMock.setItem.mockImplementationOnce(() => {
      throw new Error('quota');
    });
    const { result } = renderHook(() => useAgentId());
    expect(result.current.agentId).toMatch(ID_PATTERN);
  });

  it('does not throw when both getItem and setItem throw (Safari Private Browsing)', () => {
    localStorageMock.getItem.mockImplementation(() => {
      throw new Error('disabled');
    });
    localStorageMock.setItem.mockImplementation(() => {
      throw new Error('disabled');
    });
    expect(() => renderHook(() => useAgentId())).not.toThrow();
    const { result } = renderHook(() => useAgentId());
    expect(result.current.agentId).toMatch(ID_PATTERN);
  });
});
