import { useId } from 'react';
import type { CSSProperties } from 'react';
import { WorkerImageSelect } from './WorkerImageSelect';

export interface RemoteExecutionConfig {
  enabled?: boolean;
  worker_image?: string;
  chat_worker_image?: string;
}

export interface RemoteExecutionSectionProps {
  value: RemoteExecutionConfig;
  onChange: (next: RemoteExecutionConfig) => void;
  inputStyle: CSSProperties;
  /** Non-admins in multi mode: pickers skip their fetch and render text. */
  readOnly: boolean;
  /** Whether the chat subsystem is wired (AppConfig.chat_enabled). */
  chatEnabled: boolean;
}

export function RemoteExecutionSection({
  value,
  onChange,
  inputStyle,
  readOnly,
  chatEnabled,
}: RemoteExecutionSectionProps) {
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
          <WorkerImageSelect
            backend="agent"
            label="Task worker image"
            value={value.worker_image ?? ''}
            onChange={(img) => update({ worker_image: img || undefined })}
            readOnly={readOnly}
            inputStyle={inputStyle}
            hint="Runs cards. Must contain this project's language toolchain."
          />
        )}
        {chatEnabled && (
          <WorkerImageSelect
            backend="chat"
            label="Chat worker image"
            value={value.chat_worker_image ?? ''}
            onChange={(img) => update({ chat_worker_image: img || undefined })}
            readOnly={readOnly}
            inputStyle={inputStyle}
            hint="Chat sessions use this image even while remote execution is disabled."
          />
        )}
      </div>
    </div>
  );
}
