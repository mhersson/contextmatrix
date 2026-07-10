import { useId } from 'react';
import type { CSSProperties } from 'react';

export interface RemoteExecutionConfig {
  enabled?: boolean;
  worker_image?: string;
}

export interface RemoteExecutionSectionProps {
  value: RemoteExecutionConfig;
  onChange: (next: RemoteExecutionConfig) => void;
  inputStyle: CSSProperties;
}

export function RemoteExecutionSection({ value, onChange, inputStyle }: RemoteExecutionSectionProps) {
  const workerImageId = useId();
  const headingId = useId();

  const update = (patch: Partial<RemoteExecutionConfig>) =>
    onChange({ ...value, ...patch });

  return (
    <div>
      <div id={headingId} className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>
        Remote Execution
      </div>
      <div
        className="p-3 rounded space-y-3"
        style={{ backgroundColor: 'var(--bg1)' }}
        aria-labelledby={headingId}
      >
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={value.enabled ?? false}
            onChange={(e) => update({ enabled: e.target.checked })}
            className="accent-[var(--green)]"
          />
          <span className="text-sm" style={{ color: 'var(--fg)' }}>
            Enable remote execution
          </span>
        </label>
        {value.enabled && (
          <div>
            <label
              htmlFor={workerImageId}
              className="block text-xs mb-1"
              style={{ color: 'var(--grey1)' }}
            >
              Worker image
            </label>
            <input
              id={workerImageId}
              type="text"
              value={value.worker_image ?? ''}
              onChange={(e) => update({ worker_image: e.target.value || undefined })}
              placeholder="e.g. ghcr.io/org/worker:latest"
              className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
              style={inputStyle}
            />
            <p className="mt-1 text-xs" style={{ color: 'var(--grey1)' }}>
              Worker image must contain this project&apos;s language toolchain.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
