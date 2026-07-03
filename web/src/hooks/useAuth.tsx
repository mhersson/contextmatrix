import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { api, SESSION_EXPIRED_EVENT } from '../api/client';
import type { AuthMode, SessionUser } from '../types';

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated';

interface AuthContextValue {
  mode: AuthMode;
  status: AuthStatus;
  user: SessionUser | null;
  setUser: (u: SessionUser) => void;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

/**
 * AuthProvider resolves the deployment's auth mode and (in multi mode) the
 * current session. In "none" mode it authenticates immediately with no
 * session probe — the none-mode invariant: zero extra requests, zero UI.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<AuthMode>('none');
  const [status, setStatus] = useState<AuthStatus>('loading');
  const [user, setUserState] = useState<SessionUser | null>(null);

  useEffect(() => {
    let cancelled = false;

    api
      .getAppConfig()
      .then((config) => {
        if (cancelled) return;

        const m = config.auth_mode ?? 'none';
        setMode(m);

        if (m !== 'multi') {
          setStatus('authenticated');
          return;
        }

        api
          .getAuthSession()
          .then((u) => {
            if (cancelled) return;
            setUserState(u);
            setStatus('authenticated');
          })
          .catch(() => {
            if (cancelled) return;
            setStatus('anonymous');
          });
      })
      .catch(() => {
        // App-config unreachable: fall back to none so the app still tries
        // to render (matching today's behavior when the API is down).
        if (cancelled) return;
        setStatus('authenticated');
      });

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const onExpired = () => {
      setUserState(null);
      setStatus('anonymous');
    };

    window.addEventListener(SESSION_EXPIRED_EVENT, onExpired);
    return () => window.removeEventListener(SESSION_EXPIRED_EVENT, onExpired);
  }, []);

  const setUser = useCallback((u: SessionUser) => {
    setUserState(u);
    setStatus('authenticated');
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setUserState(null);
      setStatus('anonymous');
    }
  }, []);

  return (
    <AuthContext.Provider value={{ mode, status, user, setUser, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

/**
 * Safe accessor for consumers that may render outside AuthProvider (e.g.
 * ThemeProvider today, until App.tsx mounts AuthProvider above it). Returns
 * null instead of throwing when no provider is present.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function useOptionalAuth(): AuthContextValue | null {
  return useContext(AuthContext);
}
