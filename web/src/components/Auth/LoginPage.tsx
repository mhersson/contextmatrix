import { useState, type FormEvent } from 'react';
import { api } from '../../api/client';
import { useAuth } from '../../hooks/useAuth';
import type { APIError } from '../../types';

/** Full-screen login form shown in multi mode when no session exists. */
export function LoginPage() {
  const { setUser } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;

    setBusy(true);
    setError(null);

    try {
      const user = await api.login(username.trim(), password);
      setUser(user);
    } catch (err) {
      const apiErr = err as APIError;
      if (apiErr.code === 'RATE_LIMITED') {
        setError('Too many attempts — wait a moment and try again.');
      } else {
        // Uniform message, mirroring the server's no-oracle stance.
        setError('Invalid username or password.');
      }
      setBusy(false);
    }
  };

  return (
    <div className="h-screen flex items-center justify-center" style={{ backgroundColor: 'var(--bg-dim)' }}>
      <form
        onSubmit={submit}
        className="w-80 rounded-lg p-6 flex flex-col gap-4 border"
        style={{ backgroundColor: 'var(--bg1)', borderColor: 'var(--bg3)' }}
      >
        <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
          ContextMatrix
        </h1>

        <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
          Username
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
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
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
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  );
}
