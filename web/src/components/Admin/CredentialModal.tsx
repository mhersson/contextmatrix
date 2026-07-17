import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { api, isAPIError } from '../../api/client';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import type { CreateCredentialInput, CredentialInfo } from '../../types';
import { CredentialFields } from './CredentialFields';

interface CredentialModalProps {
  open: boolean;
  mode: 'create' | 'rotate';
  existing?: CredentialInfo;
  onClose: () => void;
  onSaved: () => void;
}

/**
 * Create / rotate dialog for a GitHub credential. Unlike CreateUserModal,
 * this component owns its own submit call and busy/error state - the fixed
 * prop contract (`{open, mode, existing?, onClose, onSaved}`) has no
 * `busy`/`error` props, so AdminCredentialsPage only reacts to `onSaved`
 * (refetch) and `onClose` (dismiss) rather than driving the request itself.
 * Field markup lives in `CredentialFields` (split out to stay under the
 * ~150-line component guideline); this file owns state, the submit call,
 * and the dialog chrome (backdrop, focus trap, error display, actions).
 */
export function CredentialModal({ open, mode, existing, onClose, onSaved }: CredentialModalProps) {
  const [kind, setKind] = useState<'pat' | 'app'>('pat');
  const [name, setName] = useState('');
  const [host, setHost] = useState('');
  const [secret, setSecret] = useState('');
  const [appId, setAppId] = useState('');
  const [installationId, setInstallationId] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [details, setDetails] = useState<string | null>(null);

  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useFocusTrap(dialogRef, open);

  // Fresh fields every time the dialog opens (render-time pattern - see
  // useBoard.ts / web/CLAUDE.md § rail sync for why this isn't a useEffect).
  // The secret/private-key field always starts blank, even in rotate mode.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setKind('pat');
      setName('');
      setHost('');
      setSecret('');
      setAppId('');
      setInstallationId('');
      setBusy(false);
      setError(null);
      setDetails(null);
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

  const isApp = mode === 'create' ? kind === 'app' : existing?.kind === 'app';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;

    setBusy(true);
    setError(null);
    setDetails(null);

    try {
      if (mode === 'create') {
        const input: CreateCredentialInput = {
          name: name.trim(),
          kind,
          secret,
          host: host.trim() || undefined,
          ...(kind === 'app' ? { app_id: Number(appId), installation_id: Number(installationId) } : {}),
        };
        await api.adminCreateCredential(input);
      } else if (existing) {
        await api.adminUpdateCredential(existing.name, { secret });
      }
      onSaved();
      onClose();
    } catch (err) {
      if (isAPIError(err)) {
        setError(err.error);
        setDetails(err.details ?? null);
      } else {
        setError('Failed to save credential.');
        setDetails(null);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={onClose} />

      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-[26rem] rounded-lg p-5 border flex flex-col gap-4"
        style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={titleId} className="text-base font-semibold" style={{ color: 'var(--fg)' }}>
          {mode === 'create' ? 'Add credential' : `Rotate secret - ${existing?.name}`}
        </h2>

        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-3">
          <CredentialFields
            mode={mode}
            existingName={existing?.name}
            isApp={isApp}
            kind={kind}
            onKindChange={setKind}
            name={name}
            onNameChange={setName}
            host={host}
            onHostChange={setHost}
            appId={appId}
            onAppIdChange={setAppId}
            installationId={installationId}
            onInstallationIdChange={setInstallationId}
            secret={secret}
            onSecretChange={setSecret}
          />

          {error && (
            <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
              {error}
              {details && (
                <div className="mt-1 text-xs" style={{ color: 'var(--grey1)' }}>
                  {details}
                </div>
              )}
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
              {busy
                ? mode === 'create'
                  ? 'Adding…'
                  : 'Rotating…'
                : mode === 'create'
                  ? 'Add credential'
                  : 'Rotate secret'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
