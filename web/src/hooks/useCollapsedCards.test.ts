import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useCollapsedCards } from './useCollapsedCards';

const STORAGE_KEY = 'contextmatrix-collapsed-cards';
const PROJECT = 'test-project';
const storageKey = `${STORAGE_KEY}-${PROJECT}`;

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock });

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

describe('useCollapsedCards', () => {
  const validIds = ['CARD-001', 'CARD-002', 'CARD-003'];

  describe('collapseMany', () => {
    it('adds multiple card IDs to the collapsed set in one call', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(['CARD-001', 'CARD-002']);
      });

      expect(result.current.collapsed.has('CARD-001')).toBe(true);
      expect(result.current.collapsed.has('CARD-002')).toBe(true);
      expect(result.current.collapsed.has('CARD-003')).toBe(false);
    });

    it('writes to localStorage exactly once per call', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(['CARD-001', 'CARD-002', 'CARD-003']);
      });

      // setItem is called once during collapseMany (not three times)
      const setItemCalls = localStorageMock.setItem.mock.calls.filter(
        ([key]) => key === storageKey,
      );
      expect(setItemCalls).toHaveLength(1);

      const saved = JSON.parse(setItemCalls[0][1]) as string[];
      expect(saved).toContain('CARD-001');
      expect(saved).toContain('CARD-002');
      expect(saved).toContain('CARD-003');
    });

    it('preserves already-collapsed cards when collapsing more', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.toggle('CARD-001');
      });
      act(() => {
        result.current.collapseMany(['CARD-002', 'CARD-003']);
      });

      expect(result.current.collapsed.has('CARD-001')).toBe(true);
      expect(result.current.collapsed.has('CARD-002')).toBe(true);
      expect(result.current.collapsed.has('CARD-003')).toBe(true);
    });
  });

  describe('expandMany', () => {
    it('removes multiple card IDs from the collapsed set in one call', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      // Collapse all first
      act(() => {
        result.current.collapseMany(validIds);
      });
      expect(result.current.collapsed.size).toBe(3);

      act(() => {
        result.current.expandMany(['CARD-001', 'CARD-003']);
      });

      expect(result.current.collapsed.has('CARD-001')).toBe(false);
      expect(result.current.collapsed.has('CARD-002')).toBe(true);
      expect(result.current.collapsed.has('CARD-003')).toBe(false);
    });

    it('writes to localStorage exactly once per call', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(validIds);
      });
      localStorageMock.setItem.mockClear();

      act(() => {
        result.current.expandMany(['CARD-001', 'CARD-002', 'CARD-003']);
      });

      const setItemCalls = localStorageMock.setItem.mock.calls.filter(
        ([key]) => key === storageKey,
      );
      expect(setItemCalls).toHaveLength(1);

      const saved = JSON.parse(setItemCalls[0][1]) as string[];
      expect(saved).toHaveLength(0);
    });

    it('is a no-op for cards not in the collapsed set', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.expandMany(['CARD-001', 'CARD-002']);
      });

      expect(result.current.collapsed.size).toBe(0);
    });
  });

  describe('interplay between toggle and bulk operations', () => {
    it('individual toggle still works after bulk collapse', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(validIds);
      });
      act(() => {
        result.current.toggle('CARD-002');
      });

      expect(result.current.collapsed.has('CARD-001')).toBe(true);
      expect(result.current.collapsed.has('CARD-002')).toBe(false);
      expect(result.current.collapsed.has('CARD-003')).toBe(true);
    });

    it('individual toggle still works after bulk expand', () => {
      const { result } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(validIds);
      });
      act(() => {
        result.current.expandMany(validIds);
      });
      act(() => {
        result.current.toggle('CARD-001');
      });

      expect(result.current.collapsed.has('CARD-001')).toBe(true);
      expect(result.current.collapsed.has('CARD-002')).toBe(false);
    });
  });

  describe('localStorage persistence', () => {
    it('collapseMany state survives re-mount via localStorage', () => {
      const { result, unmount } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(['CARD-001', 'CARD-002']);
      });
      unmount();

      const { result: result2 } = renderHook(() => useCollapsedCards(PROJECT, validIds));
      expect(result2.current.collapsed.has('CARD-001')).toBe(true);
      expect(result2.current.collapsed.has('CARD-002')).toBe(true);
      expect(result2.current.collapsed.has('CARD-003')).toBe(false);
    });

    it('expandMany state survives re-mount via localStorage', () => {
      const { result, unmount } = renderHook(() => useCollapsedCards(PROJECT, validIds));

      act(() => {
        result.current.collapseMany(validIds);
      });
      act(() => {
        result.current.expandMany(['CARD-003']);
      });
      unmount();

      const { result: result2 } = renderHook(() => useCollapsedCards(PROJECT, validIds));
      expect(result2.current.collapsed.has('CARD-001')).toBe(true);
      expect(result2.current.collapsed.has('CARD-002')).toBe(true);
      expect(result2.current.collapsed.has('CARD-003')).toBe(false);
    });
  });
});
