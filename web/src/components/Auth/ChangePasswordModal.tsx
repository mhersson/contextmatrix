import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { api } from '../../api/client';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import type { APIError } from '../../types';
import { AuthError } from './AuthError';
import { PasswordInput } from './PasswordInput';

const MIN_PASSWORD_LENGTH = 10;

interface Props {
  open: boolean;
  onClose: () => void;
}

/** Self-service password change. Other sessions are revoked server-side. */
export function ChangePasswordModal({ open, onClose }: Props) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);

  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  const reset = () => {
    setCurrent('');
    setNext('');
    setConfirm('');
    setError(null);
    setDone(false);
    setBusy(false);
  };

  const close = () => {
    reset();
    onClose();
  };

  // Focus trap, initial focus falls back to the first focusable field.
  useFocusTrap(dialogRef, open);

  // Escape key closes modal
  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') close();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (!open) return null;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;

    if (next.length < MIN_PASSWORD_LENGTH) {
      setError(`New password must be at least ${MIN_PASSWORD_LENGTH} characters.`);
      return;
    }

    if (next !== confirm) {
      setError('Passwords do not match.');
      return;
    }

    setBusy(true);
    setError(null);

    try {
      await api.changePassword(current, next);
      setDone(true);
    } catch (err) {
      const apiErr = err as APIError;
      setError(apiErr.code === 'UNAUTHORIZED' ? 'Current password is wrong.' : apiErr.error || 'Something went wrong.');
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={close} />

      {/* Dialog panel */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-96 rounded-lg p-5 border flex flex-col gap-4"
        style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={titleId} className="text-base font-semibold" style={{ color: 'var(--fg)' }}>
          Change password
        </h2>

        {done ? (
          <>
            <p className="text-sm" style={{ color: 'var(--green)' }}>
              Password changed. Your other sessions have been signed out.
            </p>
            <button onClick={close} className="auth-btn h-9 self-end px-4">
              Close
            </button>
          </>
        ) : (
          <form onSubmit={submit} className="flex flex-col">
            <PasswordInput
              label="Current password"
              value={current}
              onChange={setCurrent}
              autoComplete="current-password"
              required
            />
            <PasswordInput
              label="New password"
              hint={`min ${MIN_PASSWORD_LENGTH} chars`}
              value={next}
              onChange={setNext}
              autoComplete="new-password"
              required
            />
            <PasswordInput
              label="Confirm new password"
              value={confirm}
              onChange={setConfirm}
              autoComplete="new-password"
              required
            />

            {error && <AuthError>{error}</AuthError>}

            <div className="flex gap-2 justify-end">
              <button
                type="button"
                onClick={close}
                className="rounded-[7px] py-1.5 px-4"
                style={{ backgroundColor: 'var(--bg3)', color: 'var(--fg)' }}
              >
                Cancel
              </button>
              <button type="submit" disabled={busy} className="auth-btn h-9 px-4">
                {busy ? 'Saving…' : 'Change password'}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
