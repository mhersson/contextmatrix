import { useId } from 'react';

export interface KnowledgeSectionProps {
  /** Selected provider name, or "" for none. */
  provider: string;
  scope: string;
  /** Provider names the server configures. */
  providers: string[];
  onProviderChange: (next: string) => void;
  onScopeChange: (next: string) => void;
}

/**
 * Project-settings row for the per-project knowledge provider and scope.
 * A stored provider the server no longer configures stays selectable (and is
 * flagged) so the person can still clear it or save the project untouched.
 */
export function KnowledgeSection({
  provider,
  scope,
  providers,
  onProviderChange,
  onScopeChange,
}: KnowledgeSectionProps) {
  const selectId = useId();
  const scopeId = useId();

  if (providers.length === 0 && provider === '') {
    return <div className="ps-hint">No knowledge providers are configured on this server.</div>;
  }

  const isStale = provider !== '' && !providers.includes(provider);

  return (
    <>
      <div className="ps-field">
        <label htmlFor={selectId} className="ps-label">
          Knowledge provider
        </label>
        <select
          id={selectId}
          value={provider}
          onChange={(e) => onProviderChange(e.target.value)}
          className="bf-state-select"
        >
          <option value="">None</option>
          {providers.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
          {isStale && <option value={provider}>{`${provider} (not configured)`}</option>}
        </select>
        {isStale && (
          <div className="ps-hint ps-hint--alert" role="alert" style={{ color: 'var(--red)' }}>
            provider is not configured on this server - knowledge tools answer empty
          </div>
        )}
      </div>
      <div className="ps-field mt-3">
        <label htmlFor={scopeId} className="ps-label">
          Knowledge scope
        </label>
        <input
          id={scopeId}
          type="text"
          value={scope}
          onChange={(e) => onScopeChange(e.target.value)}
          disabled={provider === ''}
          placeholder="the product's name at the provider"
          className="bf-input"
        />
      </div>
    </>
  );
}
