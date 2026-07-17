import type { CSSProperties, ReactNode } from 'react';
import { CREDENTIAL_KIND_LABEL } from '../../lib/credentialLabels';

interface CredentialFieldsProps {
  mode: 'create' | 'rotate';
  existingName?: string;
  isApp: boolean;
  kind: 'pat' | 'app';
  onKindChange: (kind: 'pat' | 'app') => void;
  name: string;
  onNameChange: (value: string) => void;
  host: string;
  onHostChange: (value: string) => void;
  appId: string;
  onAppIdChange: (value: string) => void;
  installationId: string;
  onInstallationIdChange: (value: string) => void;
  secret: string;
  onSecretChange: (value: string) => void;
}

const fieldStyle: CSSProperties = { backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' };

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1 text-sm" style={{ color: 'var(--grey2)' }}>
      {label}
      {children}
    </label>
  );
}

/**
 * The form body of CredentialModal: kind selector + PAT/App-specific
 * fields for create mode, or the read-only name + secret-only input for
 * rotate mode. Purely presentational and fully controlled - CredentialModal
 * owns all state and the submit call. Split out to keep both files under
 * the ~150-line component guideline (web/CLAUDE.md).
 */
export function CredentialFields({
  mode,
  existingName,
  isApp,
  kind,
  onKindChange,
  name,
  onNameChange,
  host,
  onHostChange,
  appId,
  onAppIdChange,
  installationId,
  onInstallationIdChange,
  secret,
  onSecretChange,
}: CredentialFieldsProps) {
  return (
    <>
      {mode === 'create' && (
        <div className="flex gap-2">
          {(['pat', 'app'] as const).map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => onKindChange(k)}
              className="flex-1 rounded py-1.5 text-sm font-medium"
              style={
                kind === k
                  ? { backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }
                  : { backgroundColor: 'var(--bg3)', color: 'var(--grey1)' }
              }
            >
              {CREDENTIAL_KIND_LABEL[k]}
            </button>
          ))}
        </div>
      )}

      {mode === 'create' ? (
        <Field label="Name">
          <input
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            autoComplete="off"
            autoFocus
            required
            className="rounded px-2 py-1.5 border outline-none"
            style={fieldStyle}
          />
        </Field>
      ) : (
        <Field label="Name">
          <input
            value={existingName ?? ''}
            readOnly
            disabled
            className="rounded px-2 py-1.5 border outline-none"
            style={fieldStyle}
          />
        </Field>
      )}

      {mode === 'create' && (
        <Field label="Host (optional)">
          <input
            value={host}
            onChange={(e) => onHostChange(e.target.value)}
            placeholder="empty = github.com"
            autoComplete="off"
            className="rounded px-2 py-1.5 border outline-none"
            style={fieldStyle}
          />
        </Field>
      )}

      {isApp && mode === 'create' && (
        <div className="flex gap-3">
          <Field label="App ID">
            <input
              type="number"
              value={appId}
              onChange={(e) => onAppIdChange(e.target.value)}
              required
              className="rounded px-2 py-1.5 border outline-none"
              style={fieldStyle}
            />
          </Field>
          <Field label="Installation ID">
            <input
              type="number"
              value={installationId}
              onChange={(e) => onInstallationIdChange(e.target.value)}
              required
              className="rounded px-2 py-1.5 border outline-none"
              style={fieldStyle}
            />
          </Field>
        </div>
      )}

      <Field label={isApp ? 'Private key' : mode === 'create' ? 'Secret' : 'New secret'}>
        {isApp ? (
          <textarea
            value={secret}
            onChange={(e) => onSecretChange(e.target.value)}
            required
            rows={6}
            placeholder="-----BEGIN RSA PRIVATE KEY-----"
            className="rounded px-2 py-1.5 border outline-none font-mono text-xs"
            style={fieldStyle}
          />
        ) : (
          <input
            type="password"
            value={secret}
            onChange={(e) => onSecretChange(e.target.value)}
            autoComplete="new-password"
            required
            className="rounded px-2 py-1.5 border outline-none"
            style={fieldStyle}
          />
        )}
      </Field>
    </>
  );
}
