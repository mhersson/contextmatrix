import { useState, useEffect, useCallback, useMemo, useContext, createContext } from 'react';
import type { ReactNode } from 'react';
import { createElement } from 'react';
import { api } from '../api/client';
import { useOptionalAuth } from './useAuth';

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
    // Ignore - palette/theme preferences are best-effort.
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
   * Active task-execution backend from `/api/app/config`: "agent" | ""
   * (when no task backend is configured). Surfaced here because
   * ThemeProvider is the single fetcher of the app config; consumers read it
   * via `useTheme()` rather than opening a parallel fetch.
   */
  taskBackend: string;
  /**
   * Whether a chat backend is configured, from `/api/app/config`
   * (`chat_enabled`). False on the slim pre-login payload and on servers
   * older than this field. Drives the chat worker-image picker.
   */
  chatEnabled: boolean;
  /**
   * Operator-configured favorite model slugs per tier, from `/api/app/config`.
   * Key = tier name, value = All slugs for that tier. Null when the backend
   * has no favorites configured.
   */
  favorites: Record<string, string[]> | null;
  /**
   * Best-of-N bounds from `/api/app/config` (`best_of_n_max`/
   * `best_of_n_default`). Undefined on the slim pre-login payload or on
   * servers older than the best-of-n rollout - consumers apply their own
   * fallback (`?? 5` / `?? 3`).
   */
  bestOfNMax: number | undefined;
  bestOfNDefault: number | undefined;
  /**
   * Mob bounds + guest registry names from `/api/app/config`. Undefined on
   * the slim pre-login payload or on servers older than the mob rollout -
   * consumers apply their own fallback (`?? 5` / `?? 3` / `?? []`).
   */
  mobMaxParticipants: number | undefined;
  mobDefaultParticipants: number | undefined;
  mobGuestNames: string[] | undefined;
  /**
   * Whether the server allows the mob "execute" phase, from
   * `/api/app/config` (`mob_execute_checkpoints`). False on the slim
   * pre-login payload and on older servers.
   */
  mobExecuteCheckpoints: boolean;
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
  const [chatEnabled, setChatEnabled] = useState(false);
  const [favorites, setFavorites] = useState<Record<string, string[]> | null>(null);
  const [bestOfNMax, setBestOfNMax] = useState<number | undefined>(undefined);
  const [bestOfNDefault, setBestOfNDefault] = useState<number | undefined>(undefined);
  const [mobMaxParticipants, setMobMaxParticipants] = useState<number | undefined>(undefined);
  const [mobDefaultParticipants, setMobDefaultParticipants] = useState<number | undefined>(undefined);
  const [mobGuestNames, setMobGuestNames] = useState<string[] | undefined>(undefined);
  const [mobExecuteCheckpoints, setMobExecuteCheckpoints] = useState(false);

  // Optional: AuthProvider does not yet sit above ThemeProvider in App.tsx
  // (wired in a later task), and pre-existing tests render ThemeProvider
  // standalone. useOptionalAuth() returns null in both cases instead of
  // throwing.
  const user = useOptionalAuth()?.user ?? null;

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
      if (config.chat_enabled !== undefined) {
        setChatEnabled(config.chat_enabled);
      }
      if (config.favorites) {
        setFavorites(config.favorites);
      }
      if (config.best_of_n_max !== undefined) {
        setBestOfNMax(config.best_of_n_max);
      }
      if (config.best_of_n_default !== undefined) {
        setBestOfNDefault(config.best_of_n_default);
      }
      if (config.mob_max_participants !== undefined) {
        setMobMaxParticipants(config.mob_max_participants);
      }
      if (config.mob_default_participants !== undefined) {
        setMobDefaultParticipants(config.mob_default_participants);
      }
      if (config.mob_guest_names !== undefined) {
        setMobGuestNames(config.mob_guest_names);
      }
      if (config.mob_execute_checkpoints !== undefined) {
        setMobExecuteCheckpoints(config.mob_execute_checkpoints);
      }
    }).catch(() => {
      // swallow errors - leave default everforest palette
    });
    // Refetch after login: the pre-login multi-mode payload is slim
    // (theme/version/auth_mode only); task_backend and favorites arrive
    // only on the authenticated fetch.
  }, [user?.username]);

  const toggleTheme = useCallback(() => {
    setTheme((current) => (current === 'dark' ? 'light' : 'dark'));
  }, []);

  const setPalette = useCallback((p: Palette) => {
    setPaletteState(p);
    applyPalette(p);
    safeSet(PALETTE_STORAGE_KEY, p);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme, palette, version, taskBackend, chatEnabled, favorites, bestOfNMax, bestOfNDefault,
      mobMaxParticipants, mobDefaultParticipants, mobGuestNames, mobExecuteCheckpoints,
      toggleTheme, setPalette,
    }),
    [theme, palette, version, taskBackend, chatEnabled, favorites, bestOfNMax, bestOfNDefault,
      mobMaxParticipants, mobDefaultParticipants, mobGuestNames, mobExecuteCheckpoints,
      toggleTheme, setPalette],
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
