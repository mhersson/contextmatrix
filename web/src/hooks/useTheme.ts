import { useState, useEffect, useCallback, useContext, createContext } from 'react';
import type { ReactNode } from 'react';
import { createElement } from 'react';
import { api } from '../api/client';

type Theme = 'dark' | 'light';
type Palette = 'everforest' | 'radix' | 'catppuccin';

const VALID_PALETTES: readonly Palette[] = ['everforest', 'radix', 'catppuccin'];

const STORAGE_KEY = 'theme';
const PALETTE_STORAGE_KEY = 'palette';

function getInitialTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'dark' || stored === 'light') {
    return stored;
  }
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function getStoredPalette(): Palette | null {
  const stored = localStorage.getItem(PALETTE_STORAGE_KEY);
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
    return 'everforest';
  });
  const [version, setVersion] = useState('');

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, theme);
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
    localStorage.setItem(PALETTE_STORAGE_KEY, p);
  }, []);

  return createElement(ThemeContext.Provider, { value: { theme, palette, version, toggleTheme, setPalette } }, children);
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return ctx;
}
