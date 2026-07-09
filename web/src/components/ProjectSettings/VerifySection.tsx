import { useId } from 'react';
import type { CSSProperties } from 'react';

export interface VerifyConfig {
  command?: string;
  timeout_seconds?: number;
  env?: string[];
}

export interface VerifySectionProps {
  value: VerifyConfig;
  onChange: (next: VerifyConfig) => void;
  inputStyle: CSSProperties;
}

// parseEnvNames splits a comma/whitespace-separated string into distinct env
// names, dropping empties. Names are not upper-cased here — the server rejects
// malformed names so the operator sees the exact 422.
function parseEnvNames(raw: string): string[] {
  return raw
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

export function VerifySection({ value, onChange, inputStyle }: VerifySectionProps) {
  const commandId = useId();
  const timeoutId = useId();
  const envId = useId();
  const headingId = useId();

  const update = (patch: Partial<VerifyConfig>) => onChange({ ...value, ...patch });

  return (
    <div>
      <div id={headingId} className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>
        Verify
      </div>
      <div
        className="p-3 rounded space-y-3"
        style={{ backgroundColor: 'var(--bg1)' }}
        aria-labelledby={headingId}
      >
        <div>
          <label htmlFor={commandId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
            Verify command
          </label>
          <input
            id={commandId}
            type="text"
            value={value.command ?? ''}
            onChange={(e) => update({ command: e.target.value || undefined })}
            placeholder="e.g. make test"
            className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
            style={inputStyle}
          />
          <p className="mt-1 text-xs" style={{ color: 'var(--grey1)' }}>
            Shell command the agent runs to verify work (e.g. your test suite). Leave empty to let
            the agent detect or propose one.
          </p>
        </div>

        <div>
          <label htmlFor={timeoutId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
            Timeout (seconds)
          </label>
          <input
            id={timeoutId}
            type="number"
            min={0}
            value={value.timeout_seconds ?? ''}
            onChange={(e) =>
              update({ timeout_seconds: e.target.value ? Number(e.target.value) : undefined })
            }
            placeholder="600"
            className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
            style={inputStyle}
          />
        </div>

        <div>
          <label htmlFor={envId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
            Passthrough env names
          </label>
          <input
            id={envId}
            type="text"
            value={(value.env ?? []).join(', ')}
            onChange={(e) => {
              const names = parseEnvNames(e.target.value);
              update({ env: names.length > 0 ? names : undefined });
            }}
            placeholder="e.g. JAVA_HOME, CGO_ENABLED"
            className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
            style={inputStyle}
          />
          <p className="mt-1 text-xs" style={{ color: 'var(--grey1)' }}>
            Container env var names passed to the verify command. Names only, never secrets.
          </p>
        </div>
      </div>
    </div>
  );
}
