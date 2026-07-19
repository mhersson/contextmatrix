import { useId } from 'react';
import type { CSSProperties } from 'react';
import { WorkerImageSelect } from './WorkerImageSelect';

export interface RemoteExecutionConfig {
  worker_image?: string;
  chat_worker_image?: string;
}

export interface RemoteExecutionSectionProps {
  value: RemoteExecutionConfig;
  onChange: (next: RemoteExecutionConfig) => void;
  inputStyle: CSSProperties;
  /** Non-admins in multi mode: pickers skip their fetch and render text. */
  readOnly: boolean;
  /** Whether a task backend is configured (AppConfig.task_backend non-empty). */
  taskBackendConfigured: boolean;
  /** Whether a chat backend is configured (AppConfig.chat_enabled). */
  chatEnabled: boolean;
}

export function RemoteExecutionSection({
  value,
  onChange,
  inputStyle,
  readOnly,
  taskBackendConfigured,
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
        {taskBackendConfigured && (
          <WorkerImageSelect
            backend="agent"
            label="Agent worker image"
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
            hint="Chat sessions use this image."
          />
        )}
        {!taskBackendConfigured && !chatEnabled && (
          <p className="text-xs" style={{ color: 'var(--grey1)' }}>
            No execution backend is configured on this instance.
          </p>
        )}
      </div>
    </div>
  );
}
