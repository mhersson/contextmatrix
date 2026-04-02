import type { SyncStatus } from '../../types';

interface SyncIndicatorProps {
  status?: SyncStatus | null;
  onClick?: () => void;
}

function formatRelativeTime(isoString: string): string {
  const diff = Date.now() - new Date(isoString).getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

export function SyncIndicator({ status, onClick }: SyncIndicatorProps) {
  if (!status || !status.enabled) return null;

  if (status.syncing) {
    return (
      <div className="flex items-center gap-2">
        <span
          className="w-2 h-2 rounded-full animate-pulse"
          style={{ backgroundColor: 'var(--aqua)' }}
        />
        <span className="text-sm" style={{ color: 'var(--grey1)' }}>
          Syncing...
        </span>
      </div>
    );
  }

  if (status.last_sync_error) {
    return (
      <button
        onClick={onClick}
        className="flex items-center gap-2 cursor-pointer hover:opacity-80"
        title={`Sync error: ${status.last_sync_error}\nClick to retry`}
      >
        <span
          className="w-2 h-2 rounded-full"
          style={{ backgroundColor: 'var(--red)' }}
        />
        <span className="text-sm" style={{ color: 'var(--red)' }}>
          Sync error
        </span>
      </button>
    );
  }

  return (
    <button
      onClick={onClick}
      className="flex items-center gap-2 cursor-pointer hover:opacity-80"
      title="Click to sync now"
    >
      <span
        className="w-2 h-2 rounded-full"
        style={{ backgroundColor: 'var(--grey0)' }}
      />
      <span className="text-sm" style={{ color: 'var(--grey1)' }}>
        {status.last_sync_time
          ? `Synced ${formatRelativeTime(status.last_sync_time)}`
          : 'Not synced'}
      </span>
    </button>
  );
}
