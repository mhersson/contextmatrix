import { useEffect, useState, type FormEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api } from '../../api/client';
import { useAuth } from '../../hooks/useAuth';
import type { APIError, TokenInfo } from '../../types';
import { AuthError } from './AuthError';
import { AuthShell } from './AuthShell';
import { PasswordInput } from './PasswordInput';
import { TextInput } from './TextInput';

const MIN_PASSWORD_LENGTH = 10; // mirrors the server rule; server re-validates

/**
 * Redemption page for one-time links (/auth/token/<raw>). Renders by token
 * purpose: bootstrap creates the first admin (username + password), invite
 * greets the pre-created user and asks only for a password, reset asks for
 * the new password. Success = logged in, straight into the app.
 *
 * The token is derived from the pathname, NOT useParams(): AuthGate renders
 * this page by path interception above the route tree, so no <Route> ever
 * matches and useParams() would be empty (the bug that shipped originally -
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
        ? 'Set a new password'
        : `Welcome, ${info?.username}`;
  const sub =
    info?.purpose === 'bootstrap'
      ? 'First run - this account gets the admin role.'
      : info?.purpose === 'reset'
        ? `For ${info?.username}.`
        : 'Set a password to finish creating your account.';

  return (
    <AuthShell>
      {fatal ? (
        <>
          <h1 className="mb-4 text-[19px] font-semibold tracking-tight" style={{ color: 'var(--fg)' }}>
            This link cannot be used
          </h1>
          <AuthError>{fatal}</AuthError>
        </>
      ) : !info ? (
        <p className="text-sm" style={{ color: 'var(--grey1)' }}>
          Checking link…
        </p>
      ) : (
        <>
          <h1 className="text-[19px] font-semibold tracking-tight" style={{ color: 'var(--fg)' }}>
            {heading}
          </h1>
          <p className="mt-1 mb-6 text-[13px]" style={{ color: 'var(--grey1)' }}>
            {sub}
          </p>

          <form onSubmit={submit}>
            {error && <AuthError>{error}</AuthError>}

            {info.purpose === 'bootstrap' && (
              <>
                <TextInput
                  label="Username"
                  hint="a–z 0–9 . - _"
                  value={username}
                  onChange={setUsername}
                  autoComplete="username"
                  autoFocus
                  required
                />
                <TextInput label="Display name" hint="optional" value={displayName} onChange={setDisplayName} />
              </>
            )}

            <PasswordInput
              label="Password"
              hint={`min ${MIN_PASSWORD_LENGTH} chars`}
              value={password}
              onChange={setPassword}
              autoComplete="new-password"
              autoFocus={info.purpose !== 'bootstrap'}
              required
            />
            <PasswordInput
              label="Confirm password"
              value={confirm}
              onChange={setConfirm}
              autoComplete="new-password"
              required
            />

            <button type="submit" disabled={busy} className="auth-btn mt-1 h-[42px] w-full">
              {busy ? 'Saving…' : info.purpose === 'bootstrap' ? 'Create account' : 'Set password'}
            </button>
          </form>
        </>
      )}
    </AuthShell>
  );
}
