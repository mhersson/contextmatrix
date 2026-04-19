import { useState, useEffect, useCallback, useContext, createContext } from 'react';
import type { ReactNode } from 'react';
import { createElement } from 'react';
import { api } from '../api/client';

type Theme = 'dark' | 'light';
type Palette = 'everforest' | 'everforest-hard' | 'radix' | 'catppuccin';

const VALID_PALETTES: readonly Palette[] = ['everforest', 'everforest-hard', 'radix', 'catppuccin'];

const STORAGE_KEY = 'theme';

function getInitialTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'dark' || stored === 'light') {
    return stored;
  }
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
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
  toggleTheme: () => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => {
    const initial = getInitialTheme();
    applyTheme(initial);
    return initial;
  });

  const [palette, setPalette] = useState<Palette>('everforest');

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, theme);
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    api.getAppConfig().then((config) => {
      const p: Palette = VALID_PALETTES.includes(config.theme as Palette)
        ? (config.theme as Palette)
        : 'everforest';
      setPalette(p);
      applyPalette(p);
    }).catch(() => {
      // swallow errors — leave default everforest palette
    });
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme((current) => (current === 'dark' ? 'light' : 'dark'));
  }, []);

  return createElement(ThemeContext.Provider, { value: { theme, palette, toggleTheme } }, children);
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return ctx;
}
