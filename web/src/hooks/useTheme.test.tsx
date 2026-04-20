import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, act, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { ThemeProvider, useTheme } from './useTheme';

const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value; },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { store = {}; },
  };
})();
Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true });

Object.defineProperty(window, 'matchMedia', {
  configurable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

type Ctx = ReturnType<typeof useTheme>;

let latest: Ctx | null = null;

function Consumer() {
  const ctx = useTheme();
  useEffect(() => {
    latest = ctx;
  });
  return null;
}

function renderWithProvider() {
  return render(
    <ThemeProvider>
      <Consumer />
    </ThemeProvider>,
  );
}

beforeEach(() => {
  latest = null;
  localStorageMock.clear();
  vi.stubGlobal('fetch', vi.fn());
  document.documentElement.removeAttribute('data-palette');
  document.documentElement.removeAttribute('data-theme');
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute('data-palette');
  document.documentElement.removeAttribute('data-theme');
});

function mockFetchAppConfig(theme: 'everforest' | 'radix' | 'catppuccin') {
  (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
    ok: true,
    status: 200,
    json: () => Promise.resolve({ theme }),
  });
}

function mockFetchError() {
  (fetch as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('network error'));
}

describe('ThemeProvider palette', () => {
  it('sets data-palette="radix" on documentElement when server returns radix', async () => {
    mockFetchAppConfig('radix');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(document.documentElement.getAttribute('data-palette')).toBe('radix');
    });

    expect(latest!.palette).toBe('radix');
  });

  it('removes data-palette attribute when server returns everforest', async () => {
    document.documentElement.setAttribute('data-palette', 'radix');
    mockFetchAppConfig('everforest');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(document.documentElement.hasAttribute('data-palette')).toBe(false);
    });

    expect(latest!.palette).toBe('everforest');
  });

  it('leaves data-palette absent (everforest default) on fetch error', async () => {
    mockFetchError();

    await act(async () => {
      renderWithProvider();
    });

    await new Promise((r) => setTimeout(r, 10));

    expect(document.documentElement.hasAttribute('data-palette')).toBe(false);
    expect(latest!.palette).toBe('everforest');
  });

  it('exposes palette="everforest" as default before fetch resolves', () => {
    (fetch as ReturnType<typeof vi.fn>).mockReturnValue(new Promise(() => {}));

    act(() => {
      renderWithProvider();
    });

    expect(latest!.palette).toBe('everforest');
    expect(document.documentElement.hasAttribute('data-palette')).toBe(false);
  });
});

describe('ThemeProvider dark/light toggle', () => {
  it('toggleTheme switches between dark and light without affecting palette', async () => {
    mockFetchAppConfig('radix');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(document.documentElement.getAttribute('data-palette')).toBe('radix');
    });

    const initialTheme = latest!.theme;
    const flipped: 'dark' | 'light' = initialTheme === 'dark' ? 'light' : 'dark';

    act(() => {
      latest!.toggleTheme();
    });

    await waitFor(() => {
      expect(latest!.theme).toBe(flipped);
    });

    expect(document.documentElement.getAttribute('data-palette')).toBe('radix');
    expect(latest!.palette).toBe('radix');
  });
});

describe('ThemeProvider localStorage palette persistence', () => {
  it('(a) stored palette wins over server default', async () => {
    localStorageMock.setItem('palette', 'catppuccin');
    mockFetchAppConfig('radix');

    await act(async () => {
      renderWithProvider();
    });

    // Stored palette should be used immediately, server response should be ignored
    expect(latest!.palette).toBe('catppuccin');
    expect(document.documentElement.getAttribute('data-palette')).toBe('catppuccin');
  });

  it('(b) invalid stored value is ignored and server default is used', async () => {
    localStorageMock.setItem('palette', 'invalid-palette');
    mockFetchAppConfig('radix');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(latest!.palette).toBe('radix');
    });

    expect(document.documentElement.getAttribute('data-palette')).toBe('radix');
  });

  it('(c) setPalette updates DOM + localStorage + context state', async () => {
    mockFetchAppConfig('everforest');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(latest!.palette).toBe('everforest');
    });

    act(() => {
      latest!.setPalette('catppuccin');
    });

    await waitFor(() => {
      expect(latest!.palette).toBe('catppuccin');
    });

    expect(document.documentElement.getAttribute('data-palette')).toBe('catppuccin');
    expect(localStorageMock.getItem('palette')).toBe('catppuccin');
  });

  it('(d) no stored palette → server response is used', async () => {
    // No palette in localStorage
    mockFetchAppConfig('radix');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(latest!.palette).toBe('radix');
    });

    expect(document.documentElement.getAttribute('data-palette')).toBe('radix');
    // localStorage should NOT have been written by server-driven palette
    expect(localStorageMock.getItem('palette')).toBeNull();
  });
});
