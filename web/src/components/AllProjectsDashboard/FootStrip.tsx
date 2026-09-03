import type { SyncStatus } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';

interface FootStripProps {
  version: string | null;
  syncStatus: SyncStatus | null;
}

function isOffline(status: SyncStatus | null): boolean {
  return status?.shared === true && status.remote_reachable === false;
}

function syncLine(status: SyncStatus | null): string {
  if (!status) return 'sync unknown';
  if (!status.enabled) return 'sync disabled';
  if (isOffline(status)) return 'sync offline';
  if (status.claims_at_risk) return 'pushes failing · claims at risk';
  if (status.last_sync_error) return 'sync error';
  if (status.syncing) return 'syncing…';
  let line = status.last_sync_time
    ? `synced ${formatRelativeTime(status.last_sync_time)}`
    : 'not yet synced';
  const unpushed = status.unpushed_commits ?? 0;
  if (unpushed > 0) {
    line += ` · ${unpushed} unpushed`;
  }
  return line;
}

function systemsLabel(status: SyncStatus | null): { label: string; color: string } {
  if (isOffline(status)) {
    return { label: 'Sync offline', color: 'var(--red)' };
  }
  if (status?.claims_at_risk) {
    return { label: 'Claims at risk', color: 'var(--red)' };
  }
  if (status?.last_sync_error) {
    return { label: 'Sync degraded', color: 'var(--red)' };
  }
  if (!status?.enabled) {
    return { label: 'Sync disabled', color: 'var(--grey1)' };
  }
  const unpushed = status?.unpushed_commits ?? 0;
  if (unpushed > 0) {
    return { label: `${unpushed} unpushed`, color: 'var(--yellow)' };
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
      <span title={(isOffline(syncStatus) ? syncStatus?.last_remote_error : syncStatus?.last_sync_error) || undefined}>
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
