import type { SyncStatus } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';

interface FootStripProps {
  version: string | null;
  syncStatus: SyncStatus | null;
}

function syncLine(status: SyncStatus | null): string {
  if (!status) return 'unknown';
  if (!status.enabled) return 'disabled';
  if (status.last_sync_error) return 'error';
  if (status.syncing) return 'syncing…';
  if (status.last_sync_time) {
    return `enabled · ${formatRelativeTime(status.last_sync_time)}`;
  }
  return 'enabled · not yet synced';
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
        fontSize: 11,
        color: 'var(--grey1)',
        borderTop: '1px solid var(--bg3)',
        backgroundColor: 'var(--bg-dim)',
        flexShrink: 0,
        gap: 12,
      }}
    >
      <div className="flex items-center gap-4 flex-wrap">
        <span>
          <span style={{ color: 'var(--grey2)', fontWeight: 500 }}>ContextMatrix</span>{' '}
          {version ? `v${formatVersionWithLocalTime(version)}` : 'dev'}
        </span>
      </div>
      <div className="flex items-center gap-4 flex-wrap">
        <span>
          Sync <span style={{ color: 'var(--grey2)', fontWeight: 500 }}>{syncLine(syncStatus)}</span>
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
    </div>
  );
}
