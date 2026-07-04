import { useEffect, useState, type FormEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api } from '../../api/client';
import { useAuth } from '../../hooks/useAuth';
import type { APIError, TokenInfo } from '../../types';

const MIN_PASSWORD_LENGTH = 10; // mirrors the server rule; server re-validates

/**
 * Redemption page for one-time links (/auth/token/<raw>). Renders by token
 * purpose: bootstrap creates the first admin (username + password), invite
 * greets the pre-created user and asks only for a password, reset asks for
 * the new password. Success = logged in, straight into the app.
 *
 * The token is derived from the pathname, NOT useParams(): AuthGate renders
 * this page by path interception above the route tree, so no <Route> ever
 * matches and useParams() would be empty (the bug that shipped originally —
 * every redemption 404ed on an empty token).
 */
export function TokenRedemptionPage() {
  const location = useLocation();
  const token = decodeURIComponent(location.pathname.replace(/^\/auth\/token\//, ''));
  const navigate = useNavigate();
  const { setUser } = useAuth();

  const [info, setInfo] = useState<TokenInfo | null>(null);
  const [fatal, setFatal] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;

    api
      .inspectToken(token)
      .then((t) => {
        if (!cancelled) setInfo(t);
      })
      .catch((err: APIError) => {
        if (cancelled) return;
        setFatal(
          err.code === 'TOKEN_INVALID' && err.error !== 'unknown link'
            ? 'This link has already been used or has expired. Ask an admin for a new one.'
            : 'This link is not valid.'
        );
      });

    return () => {
      cancelled = true;
    };
  }, [token]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !info) return;

    if (password.length < MIN_PASSWORD_LENGTH) {
      setError(`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`);
      return;
    }

    if (password !== confirm) {
      setError('Passwords do not match.');
      return;
    }

    setBusy(true);
    setError(null);

    try {
      const user = await api.redeemToken(token, {
        password,
        ...(info.purpose === 'bootstrap' ? { username: username.trim(), display_name: displayName.trim() } : {}),
      });
      setUser(user);
      navigate('/', { replace: true });
    } catch (err) {
      const apiErr = err as APIError;
      setError(apiErr.details || apiErr.error || 'Something went wrong.');
      setBusy(false);
    }
  };

  const heading =
    info?.purpose === 'bootstrap'
      ? 'Create the admin account'
      : info?.purpose === 'reset'
        ? `Set a new password for ${info.username}`
        : info
          ? `Welcome, ${info.username} — set your password`
          : 'Checking link…';

  return (
    <div className="h-screen flex items-center justify-center" style={{ backgroundColor: 'var(--bg-dim)' }}>
      <div
        className="w-96 rounded-lg p-6 flex flex-col gap-4 border"
        style={{ backgroundColor: 'var(--bg1)', borderColor: 'var(--bg3)' }}
      >
        <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
          {heading}
        </h1>

        {fatal ? (
          <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
            {fatal}
          </div>
        ) : !info ? null : (
          <form onSubmit={submit} className="flex flex-col gap-4">
            {info.purpose === 'bootstrap' && (
              <>
                <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
                  Username (a–z, 0–9, dot, dash, underscore)
                  <input
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    autoComplete="username"
                    autoFocus
                    required
                    className="rounded px-2 py-1.5 border outline-none"
                    style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
                  />
                </label>
                <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
                  Display name (optional)
                  <input
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    className="rounded px-2 py-1.5 border outline-none"
                    style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
                  />
                </label>
              </>
            )}

            <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
              Password (min {MIN_PASSWORD_LENGTH} characters)
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                autoFocus={info.purpose !== 'bootstrap'}
                required
                className="rounded px-2 py-1.5 border outline-none"
                style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
              />
            </label>

            <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
              Confirm password
              <input
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="new-password"
                required
                className="rounded px-2 py-1.5 border outline-none"
                style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
              />
            </label>

            {error && (
              <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={busy}
              className="rounded py-1.5 font-medium disabled:opacity-60"
              style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)' }}
            >
              {busy ? 'Saving…' : info.purpose === 'bootstrap' ? 'Create account' : 'Set password'}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
