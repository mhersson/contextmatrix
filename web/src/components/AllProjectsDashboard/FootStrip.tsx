import type { SyncStatus } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';

interface FootStripProps {
  version: string | null;
  syncStatus: SyncStatus | null;
}

function syncLine(status: SyncStatus | null): string {
  if (!status) return 'sync unknown';
  if (!status.enabled) return 'sync disabled';
  if (status.last_sync_error) return 'sync error';
  if (status.syncing) return 'syncing…';
  if (status.last_sync_time) {
    return `synced ${formatRelativeTime(status.last_sync_time)}`;
  }
  return 'not yet synced';
}

function systemsLabel(status: SyncStatus | null): { label: string; color: string } {
  if (status?.last_sync_error) {
    return { label: 'Sync degraded', color: 'var(--red)' };
  }
  if (!status?.enabled) {
    return { label: 'Sync disabled', color: 'var(--grey1)' };
  }
  return { label: 'All systems operational', color: 'var(--green)' };
}

export function FootStrip({ version, syncStatus }: FootStripProps) {
  const sys = systemsLabel(syncStatus);
  return (
    <div
      className="apd-foot-strip flex flex-wrap items-center justify-between"
      style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 10.5,
        color: 'var(--grey1)',
        borderTop: '1px solid var(--bg1)',
        backgroundColor: 'var(--bg-dim)',
        flexShrink: 0,
        gap: 12,
      }}
    >
      <span title={syncStatus?.last_sync_error || undefined}>
        <span style={{ color: 'var(--grey2)', fontWeight: 500 }}>ContextMatrix</span>{' '}
        {version ? `v${formatVersionWithLocalTime(version)}` : 'dev'} · {syncLine(syncStatus)}
      </span>
      <span>
        <span
          aria-hidden="true"
          style={{
            display: 'inline-block',
            width: 6,
            height: 6,
            borderRadius: '50%',
            backgroundColor: sys.color,
            marginRight: 6,
            verticalAlign: 'middle',
          }}
        />
        {sys.label}
      </span>
    </div>
  );
}
