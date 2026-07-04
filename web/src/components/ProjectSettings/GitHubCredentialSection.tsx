import { useEffect, useId, useState } from 'react';
import { api, isAPIError } from '../../api/client';
import type { CredentialInfo } from '../../types';

interface GitHubCredentialSectionProps {
  /** Bound credential-pool entry name, or "" for the instance default. */
  value: string;
  onChange: (next: string) => void;
  /** Non-admins in multi mode: render plain text, skip the pool fetch. */
  readOnly: boolean;
}

const INSTANCE_DEFAULT_LABEL = 'Instance default (github.* config)';

function credentialLabel(c: CredentialInfo): string {
  const host = c.host || 'github.com';
  const suffix = c.disabled ? ' (disabled)' : '';
  return `${c.name} — ${c.kind}, ${host}${suffix}`;
}

/**
 * Project-settings row for the per-project GitHub credential binding
 * (`ProjectConfig.github_credential`). Admins get a `<select>` populated
 * from the instance credential pool (`adminListCredentials`, admin-only
 * server-side — the fetch is skipped entirely in readOnly mode since a
 * non-admin call would 403 anyway). Non-admins get a plain-text row
 * derived from the project config alone.
 *
 * A bound name that no longer exists in the pool is a fail-closed
 * condition server-side (operations on the project will fail) — the
 * warning surfaces that inline rather than silently substituting the
 * instance default, and the stale option is kept in the `<select>` so
 * saving without touching this field is still possible.
 */
export function GitHubCredentialSection({ value, onChange, readOnly }: GitHubCredentialSectionProps) {
  const [credentials, setCredentials] = useState<CredentialInfo[]>([]);
  const [loading, setLoading] = useState(!readOnly);
  const [error, setError] = useState<string | null>(null);
  const selectId = useId();

  useEffect(() => {
    if (readOnly) return;
    let cancelled = false;
    api
      .adminListCredentials()
      .then((list) => {
        if (cancelled) return;
        setCredentials(list);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(isAPIError(err) ? err.error : 'Failed to load credentials');
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [readOnly]);

  if (readOnly) {
    return (
      <div>
        <div className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
          GitHub credential
        </div>
        <div className="px-3 py-2 rounded text-sm" style={{ backgroundColor: 'var(--bg1)', color: 'var(--grey2)' }}>
          {value || 'Instance default'}
        </div>
      </div>
    );
  }

  const isMissing = !loading && !error && value !== '' && !credentials.some((c) => c.name === value);

  return (
    <div>
      <label htmlFor={selectId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
        GitHub credential
      </label>
      <select
        id={selectId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
        style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
      >
        <option value="">{INSTANCE_DEFAULT_LABEL}</option>
        {credentials.map((c) => (
          <option key={c.name} value={c.name}>
            {credentialLabel(c)}
          </option>
        ))}
        {isMissing && <option value={value}>{`${value} (credential missing)`}</option>}
      </select>
      {error && (
        <div className="text-xs mt-1" style={{ color: 'var(--red)' }}>
          {error}
        </div>
      )}
      {isMissing && (
        <div className="text-xs mt-1" role="alert" style={{ color: 'var(--red)' }}>
          credential no longer exists — operations on this project will fail
        </div>
      )}
    </div>
  );
}
