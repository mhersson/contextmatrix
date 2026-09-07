import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, act, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { ThemeProvider, useTheme } from './useTheme';
import type { Palette } from '../lib/palettes';

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

function mockFetchAppConfig(theme: Palette) {
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
  it('sets data-palette="github" on documentElement when server returns github', async () => {
    mockFetchAppConfig('github');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(document.documentElement.getAttribute('data-palette')).toBe('github');
    });

    expect(latest!.palette).toBe('github');
  });

  it('removes data-palette attribute when server returns everforest', async () => {
    document.documentElement.setAttribute('data-palette', 'github');
    mockFetchAppConfig('everforest');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(document.documentElement.hasAttribute('data-palette')).toBe(false);
    });

    expect(latest!.palette).toBe('everforest');
  });

  it('leaves data-palette unset on fetch error (everforest default)', async () => {
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

describe('ThemeProvider dark/light selection', () => {
  it('setTheme applies the chosen mode without affecting palette', async () => {
    mockFetchAppConfig('github');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(document.documentElement.getAttribute('data-palette')).toBe('github');
    });

    act(() => {
      latest!.setTheme('light');
    });
    await waitFor(() => {
      expect(latest!.theme).toBe('light');
    });
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(localStorageMock.getItem('theme')).toBe('light');

    act(() => {
      latest!.setTheme('dark');
    });
    await waitFor(() => {
      expect(latest!.theme).toBe('dark');
    });
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
    expect(localStorageMock.getItem('theme')).toBe('dark');

    expect(document.documentElement.getAttribute('data-palette')).toBe('github');
    expect(latest!.palette).toBe('github');
  });

  it('setTheme with the current mode is a no-op', async () => {
    mockFetchAppConfig('everforest');
    await act(async () => {
      renderWithProvider();
    });
    const before = latest!.theme;
    act(() => {
      latest!.setTheme(before);
    });
    expect(latest!.theme).toBe(before);
  });
});

describe('ThemeProvider localStorage palette persistence', () => {
  it('(a) stored palette wins over server default', async () => {
    localStorageMock.setItem('palette', 'catppuccin');
    mockFetchAppConfig('github');

    await act(async () => {
      renderWithProvider();
    });

    // Stored palette should be used immediately, server response should be ignored
    expect(latest!.palette).toBe('catppuccin');
    expect(document.documentElement.getAttribute('data-palette')).toBe('catppuccin');
  });

  it('(b) a stored value that is no longer a palette (radix) is ignored and server default is used', async () => {
    localStorageMock.setItem('palette', 'radix');
    mockFetchAppConfig('github');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(latest!.palette).toBe('github');
    });

    expect(document.documentElement.getAttribute('data-palette')).toBe('github');
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
    mockFetchAppConfig('github');

    await act(async () => {
      renderWithProvider();
    });

    await waitFor(() => {
      expect(latest!.palette).toBe('github');
    });

    expect(document.documentElement.getAttribute('data-palette')).toBe('github');
    // localStorage should NOT have been written by server-driven palette
    expect(localStorageMock.getItem('palette')).toBeNull();
  });
});
