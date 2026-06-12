import { useState, useEffect, useCallback, useMemo, useContext, createContext } from 'react';
import type { ReactNode } from 'react';
import { createElement } from 'react';
import { api } from '../api/client';

type Theme = 'dark' | 'light';
type Palette = 'everforest' | 'radix' | 'catppuccin';

const VALID_PALETTES: readonly Palette[] = ['everforest', 'radix', 'catppuccin'];

const STORAGE_KEY = 'theme';
const PALETTE_STORAGE_KEY = 'palette';

// Safari Private Browsing and some embedded contexts throw on any localStorage
// access. ThemeProvider is the outermost provider in App.tsx, so a throw here
// crashes the entire app. Wrap all reads and writes defensively.
function safeGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Ignore — palette/theme preferences are best-effort.
  }
}

function getInitialTheme(): Theme {
  const stored = safeGet(STORAGE_KEY);
  if (stored === 'dark' || stored === 'light') {
    return stored;
  }
  return 'light';
}

function getStoredPalette(): Palette | null {
  const stored = safeGet(PALETTE_STORAGE_KEY);
  if (stored !== null && VALID_PALETTES.includes(stored as Palette)) {
    return stored as Palette;
  }
  return null;
}

function applyTheme(theme: Theme) {
  if (theme === 'light') {
    document.documentElement.setAttribute('data-theme', 'light');
  } else {
    document.documentElement.removeAttribute('data-theme');
  }
}

function applyPalette(palette: Palette) {
  if (palette === 'everforest') {
    document.documentElement.removeAttribute('data-palette');
  } else {
    document.documentElement.setAttribute('data-palette', palette);
  }
}

interface ThemeContextValue {
  theme: Theme;
  palette: Palette;
  version: string;
  /**
   * Active task-execution backend from `/api/app/config`: "runner" | "agent"
   * | "" (when no task backend is configured). Surfaced here because
   * ThemeProvider is the single fetcher of the app config; consumers read it
   * via `useTheme()` rather than opening a parallel fetch.
   */
  taskBackend: string;
  toggleTheme: () => void;
  setPalette: (palette: Palette) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => {
    const initial = getInitialTheme();
    applyTheme(initial);
    return initial;
  });

  const [palette, setPaletteState] = useState<Palette>(() => {
    const stored = getStoredPalette();
    if (stored !== null) {
      applyPalette(stored);
      return stored;
    }
    applyPalette('everforest');
    return 'everforest';
  });
  const [version, setVersion] = useState('');
  const [taskBackend, setTaskBackend] = useState('');

  useEffect(() => {
    safeSet(STORAGE_KEY, theme);
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    const stored = getStoredPalette();
    api.getAppConfig().then((config) => {
      if (stored === null) {
        const p: Palette = VALID_PALETTES.includes(config.theme as Palette)
          ? (config.theme as Palette)
          : 'everforest';
        setPaletteState(p);
        applyPalette(p);
      }
      if (config.version) {
        setVersion(config.version);
      }
      if (config.task_backend) {
        setTaskBackend(config.task_backend);
      }
    }).catch(() => {
      // swallow errors — leave default everforest palette
    });
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme((current) => (current === 'dark' ? 'light' : 'dark'));
  }, []);

  const setPalette = useCallback((p: Palette) => {
    setPaletteState(p);
    applyPalette(p);
    safeSet(PALETTE_STORAGE_KEY, p);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ theme, palette, version, taskBackend, toggleTheme, setPalette }),
    [theme, palette, version, taskBackend, toggleTheme, setPalette],
  );

  return createElement(ThemeContext.Provider, { value }, children);
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return ctx;
}
