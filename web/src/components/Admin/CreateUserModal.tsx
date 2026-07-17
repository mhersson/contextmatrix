import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';

export interface CreateUserInput {
  username: string;
  display_name?: string;
  is_admin?: boolean;
}

interface CreateUserModalProps {
  open: boolean;
  busy: boolean;
  error: string | null;
  onClose: () => void;
  onCreate: (input: CreateUserInput) => void;
}

/**
 * Presentational new-user form. AdminUsersPage owns the adminCreateUser
 * call, the busy/error state, and what happens on success (opening
 * InviteLinkDialog) - this component only collects input and reports it.
 */
export function CreateUserModal({ open, busy, error, onClose, onCreate }: CreateUserModalProps) {
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [isAdmin, setIsAdmin] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useFocusTrap(dialogRef, open);

  // Fresh fields every time the dialog opens (render-time pattern - see
  // useBoard.ts / web/CLAUDE.md § rail sync for why this isn't a useEffect).
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setUsername('');
      setDisplayName('');
      setIsAdmin(false);
    }
  }

  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (busy || !username.trim()) return;

    onCreate({
      username: username.trim(),
      display_name: displayName.trim() || undefined,
      is_admin: isAdmin,
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={onClose} />

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
          New user
        </h2>

        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
            Username
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="off"
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
              autoComplete="off"
              className="rounded px-2 py-1.5 border outline-none"
              style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
            />
          </label>

          <label className="flex items-center gap-2 text-sm" style={{ color: 'var(--grey2)' }}>
            <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
            Admin
          </label>

          {error && (
            <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
              {error}
            </div>
          )}

          <div className="flex gap-2 justify-end">
            <button
              type="button"
              onClick={onClose}
              className="rounded py-1.5 px-4"
              style={{ backgroundColor: 'var(--bg3)', color: 'var(--fg)' }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={busy}
              className="rounded py-1.5 px-4 font-medium disabled:opacity-60"
              style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)' }}
            >
              {busy ? 'Creating…' : 'Create user'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
