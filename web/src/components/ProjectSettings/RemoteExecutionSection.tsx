import { WorkerImageSelect } from './WorkerImageSelect';

export interface RemoteExecutionConfig {
  worker_image?: string;
  chat_worker_image?: string;
}

export interface RemoteExecutionSectionProps {
  value: RemoteExecutionConfig;
  onChange: (next: RemoteExecutionConfig) => void;
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
  readOnly,
  taskBackendConfigured,
  chatEnabled,
}: RemoteExecutionSectionProps) {
  const update = (patch: Partial<RemoteExecutionConfig>) =>
    onChange({ ...value, ...patch });

  if (!taskBackendConfigured && !chatEnabled) {
    return <p className="ps-hint">No execution backend is configured on this instance.</p>;
  }

  return (
    <div className="ps-two">
      {taskBackendConfigured && (
        <WorkerImageSelect
          backend="agent"
          label="Agent worker image"
          value={value.worker_image ?? ''}
          onChange={(img) => update({ worker_image: img || undefined })}
          readOnly={readOnly}
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
          hint="Chat sessions use this image."
        />
      )}
    </div>
  );
}
