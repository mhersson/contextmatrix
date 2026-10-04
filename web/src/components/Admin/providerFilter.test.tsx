import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, renderHook, screen } from '@testing-library/react';
import type { SelectorCandidate } from '../../types';
import { OTHER_PROVIDER, PROVIDER_FILTER_KEY, providerCounts, providerOf, useProviderFilter } from './providerFilter';
import { ProviderFilter } from './ProviderFilter';

function cand(slug: string, creator: string): SelectorCandidate {
  return { slug, creator, coder_prior: 0.9, reviewer_prior: 0.9, prompt_price_per_tok: 1e-6, completion_price_per_tok: 1e-6, context_window: 200000, price_source: 'gateway', scored_from: '', coder_prior_estimated: false };
}

// Real backing store + spy-able methods, matching the pattern in
// useChatFilterPrefs.test.ts, so throwing-storage tests can mock one method.
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

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true });

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('providerOf', () => {
  it('groups creator-empty candidates by slug prefix', () => {
    expect(providerOf({ slug: 'zhipu/glm-4', creator: '' })).toBe('zhipu');
  });

  it('groups slug candidates without a prefix under other', () => {
    expect(providerOf({ slug: 'noslash', creator: '' })).toBe(OTHER_PROVIDER);
  });

  it('uses a non-empty trimmed creator over the slug prefix', () => {
    expect(providerOf({ slug: 'a/model', creator: '  Meta  ' })).toBe('Meta');
    expect(providerOf({ slug: 'a/model', creator: '   ' })).toBe('a');
  });
});

describe('providerCounts', () => {
  it('sorts providers alphabetically with their candidate counts', () => {
    const candidates = [cand('openai/gpt', ''), cand('b/x', ''), cand('anthropic/x', 'Anthropic'), cand('a/x', ''), cand('openai/gpt2', '')];
    expect(providerCounts(candidates)).toEqual([
      { provider: 'a', count: 1 },
      { provider: 'Anthropic', count: 1 },
      { provider: 'b', count: 1 },
      { provider: 'openai', count: 2 },
    ]);
  });

  it('sorts case-insensitively', () => {
    const candidates = [cand('a/x', 'apple'), cand('b/y', 'Bash')];
    expect(providerCounts(candidates).map((p) => p.provider)).toEqual(['apple', 'Bash']);
  });
});

describe('useProviderFilter', () => {
  it('hides candidates only for unchecked providers via visible()', () => {
    localStorageMock.setItem(PROVIDER_FILTER_KEY, JSON.stringify(['Anthropic']));
    const { result } = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic'), cand('b/y', 'openai')]));

    expect(result.current.hidden).toEqual(new Set(['Anthropic']));
    expect(result.current.visible(cand('a/x', 'Anthropic'))).toBe(false);
    expect(result.current.visible(cand('b/y', 'openai'))).toBe(true);
    expect(result.current.isActive).toBe(true);
  });

  it('a provider new to the catalog defaults to checked even with a stored hidden set', () => {
    localStorageMock.setItem(PROVIDER_FILTER_KEY, JSON.stringify(['anthropic']));
    const { result } = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic'), cand('b/y', 'Deepseek')]));

    expect(result.current.visible(cand('b/y', 'Deepseek'))).toBe(true);
  });

  it('selection survives a remount through localStorage', () => {
    const first = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic'), cand('b/y', 'openai')]));
    act(() => first.result.current.toggle('openai'));
    expect(localStorageMock.getItem(PROVIDER_FILTER_KEY)).toBe(JSON.stringify(['openai']));
    first.unmount();

    const second = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic'), cand('b/y', 'openai')]));
    expect(second.result.current.hidden).toEqual(new Set(['openai']));
  });

  it('selectNone hides all current providers; selectAll clears the set', () => {
    const { result } = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic'), cand('b/y', 'openai')]));

    act(() => result.current.selectNone());
    expect(result.current.hidden).toEqual(new Set(['Anthropic', 'openai']));
    expect(result.current.isActive).toBe(true);

    act(() => result.current.selectAll());
    expect(result.current.hidden.size).toBe(0);
    expect(result.current.isActive).toBe(false);
  });

  it('tolerates malformed stored JSON and falls back to everything checked', () => {
    localStorageMock.setItem(PROVIDER_FILTER_KEY, 'not-json{{{');
    const { result } = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic')]));

    expect(result.current.hidden.size).toBe(0);
    expect(result.current.isActive).toBe(false);
  });

  it('tolerates a throwing getItem and setItem without crashing', () => {
    localStorageMock.getItem.mockImplementationOnce(() => {
      throw new Error('storage blocked');
    });
    const { result } = renderHook(() => useProviderFilter([cand('a/x', 'Anthropic')]));
    expect(result.current.hidden.size).toBe(0);

    localStorageMock.setItem.mockImplementationOnce(() => {
      throw new Error('QuotaExceededError');
    });
    expect(() => act(() => result.current.toggle('Anthropic'))).not.toThrow();
    expect(result.current.hidden).toEqual(new Set(['Anthropic']));
  });
});

describe('ProviderFilter component', () => {
  const CANDIDATES = [cand('anthropic/x', 'Anthropic'), cand('anthropic/y', 'Anthropic'), cand('openai/z', 'openai')];

  it('renders one checkbox per provider with its count and accessible group name', () => {
    render(<ProviderFilter candidates={CANDIDATES} />);

    const group = screen.getByLabelText('Provider filter');
    expect(group).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Anthropic 2' })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'openai 1' })).toBeChecked();
    expect(screen.getByRole('button', { name: 'all' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'none' })).toBeInTheDocument();
  });

  it('toggling a checkbox hides that provider and updates the other checkboxes', () => {
    render(<ProviderFilter candidates={CANDIDATES} />);

    fireEvent.click(screen.getByRole('checkbox', { name: 'Anthropic 2' }));
    expect(screen.getByRole('checkbox', { name: 'Anthropic 2' })).not.toBeChecked();
    expect(localStorageMock.getItem(PROVIDER_FILTER_KEY)).toBe(JSON.stringify(['Anthropic']));
  });

  it('none followed by checking one provider leaves only that provider visible', () => {
    render(<ProviderFilter candidates={CANDIDATES} />);

    fireEvent.click(screen.getByRole('button', { name: 'none' }));
    expect(screen.getByRole('checkbox', { name: 'Anthropic 2' })).not.toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'openai 1' })).not.toBeChecked();

    fireEvent.click(screen.getByRole('checkbox', { name: 'openai 1' }));

    const { result } = renderHook(() => useProviderFilter(CANDIDATES));
    expect(result.current.hidden).toEqual(new Set(['Anthropic']));
    expect(result.current.visible(CANDIDATES[0])).toBe(false);
    expect(result.current.visible(CANDIDATES[2])).toBe(true);
  });

  it('the selection survives a remount through localStorage', () => {
    const { unmount } = render(<ProviderFilter candidates={CANDIDATES} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Anthropic 2' }));
    unmount();

    render(<ProviderFilter candidates={CANDIDATES} />);
    expect(screen.getByRole('checkbox', { name: 'Anthropic 2' })).not.toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'openai 1' })).toBeChecked();
  });

  it('a throwing localStorage still renders and toggles without error', () => {
    localStorageMock.getItem.mockImplementation(() => {
      throw new Error('storage blocked');
    });
    localStorageMock.setItem.mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    render(<ProviderFilter candidates={CANDIDATES} />);

    expect(screen.getByRole('checkbox', { name: 'Anthropic 2' })).toBeChecked();
    expect(() => fireEvent.click(screen.getByRole('checkbox', { name: 'Anthropic 2' }))).not.toThrow();
    expect(screen.getByRole('checkbox', { name: 'Anthropic 2' })).not.toBeChecked();
  });
});
