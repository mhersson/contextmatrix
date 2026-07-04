import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { api, isAPIError, SESSION_EXPIRED_EVENT } from '../api/client';
import type { AppConfig, AuthMode, SessionUser } from '../types';

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated';

// Bounded retry around the initial app-config fetch. This call decides the
// auth mode (multi vs none), so a single transient blip against a multi-mode
// server must not mis-detect it as none-mode and drop the user onto the
// no-auth path. 3 attempts total, short backoff between them (adds at most
// ~400ms in the worst case before falling back).
const APP_CONFIG_RETRY_BACKOFFS_MS = [100, 300];

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// A transient failure is worth retrying; a definite one is not.
//
// - No structured APIError shape at all means the request never reached (or
//   never got a response from) the server: offline, DNS failure, connection
//   refused, or an aborted timeout. Always transient.
// - `code === 'UNKNOWN_ERROR'` is the API client's synthesized fallback
//   (api/client.ts) used when a response body fails to parse as JSON.
//   `/api/app/config` is session-exempt (internal/api/auth.go's
//   sessionExempt) and its handler always responds 200 with valid JSON
//   (internal/api/app_config.go) — it cannot itself produce a 401, or any
//   other structured error. An UNKNOWN_ERROR here therefore means something
//   in front of the app (reverse proxy, load balancer) returned a non-JSON
//   error page, almost always a transient mid-deploy 5xx. Worth retrying.
// - Any other structured error code would mean the app itself rejected the
//   request, which is structurally impossible on this route today; treated
//   as non-transient defensively (fail fast rather than retry something
//   that can't be transient — this is also why a 401 specifically is never
//   retried, though it cannot occur on this exempt route).
function isTransientAppConfigError(err: unknown): boolean {
  if (!isAPIError(err)) return true;
  return err.code === 'UNKNOWN_ERROR';
}

async function getAppConfigWithRetry(): Promise<AppConfig> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await api.getAppConfig();
    } catch (err) {
      if (attempt >= APP_CONFIG_RETRY_BACKOFFS_MS.length || !isTransientAppConfigError(err)) {
        throw err;
      }
      await sleep(APP_CONFIG_RETRY_BACKOFFS_MS[attempt]);
    }
  }
}

interface AuthContextValue {
  mode: AuthMode;
  status: AuthStatus;
  user: SessionUser | null;
  /** Server version from app config; null until resolved (or when unreachable). */
  version: string | null;
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
  const [version, setVersion] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    getAppConfigWithRetry()
      .then((config) => {
        if (cancelled) return;

        const m = config.auth_mode ?? 'none';
        setMode(m);
        setVersion(config.version || null);

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
    // Best-effort: the state flip below is the contract. A failed request
    // (e.g. session already dead) must not surface as an unhandled
    // rejection to callers that fire-and-forget (`void logout()`).
    try {
      await api.logout();
    } catch {
      // ignore — state flip in finally still runs
    } finally {
      setUserState(null);
      setStatus('anonymous');
    }
  }, []);

  return (
    <AuthContext.Provider value={{ mode, status, user, version, setUser, logout }}>
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
