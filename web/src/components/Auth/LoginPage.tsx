import { useState, type FormEvent } from 'react';
import { api } from '../../api/client';
import { useAuth } from '../../hooks/useAuth';
import type { APIError } from '../../types';
import { AuthError } from './AuthError';
import { AuthShell } from './AuthShell';
import { PasswordInput } from './PasswordInput';
import { TextInput } from './TextInput';

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
        setError('Too many attempts - wait a moment and try again.');
      } else {
        // Uniform message, mirroring the server's no-oracle stance.
        setError('Invalid username or password.');
      }
      setBusy(false);
    }
  };

  return (
    <AuthShell hint="Lost access? Ask an admin for a reset link.">
      <h1 className="text-[19px] font-semibold tracking-tight" style={{ color: 'var(--fg)' }}>
        Sign in
      </h1>
      <p className="mt-1 mb-6 text-[13px]" style={{ color: 'var(--grey1)' }}>
        Use your board account.
      </p>

      <form onSubmit={submit}>
        {error && <AuthError>{error}</AuthError>}

        <TextInput label="Username" value={username} onChange={setUsername} autoComplete="username" autoFocus required />
        <PasswordInput label="Password" value={password} onChange={setPassword} autoComplete="current-password" required />

        <button type="submit" disabled={busy} className="auth-btn mt-1 h-[42px] w-full">
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </AuthShell>
  );
}
