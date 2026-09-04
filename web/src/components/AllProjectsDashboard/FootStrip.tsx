import type { SyncStatus } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';

interface FootStripProps {
  version: string | null;
  syncStatuses: SyncStatus[];
}

function isOffline(status: SyncStatus | null): boolean {
  return status?.shared === true && status.remote_reachable === false;
}

// Only a shared repo can put claims at risk. Gating on `shared` as well as the
// flag keeps a private board silent if the field is ever stale or reused, and
// mirrors the condition BoardFooter applies.
function isAtRisk(status: SyncStatus | null): boolean {
  return status?.shared === true && status.claims_at_risk === true;
}

function syncLine(status: SyncStatus | null): string {
  if (!status) return 'sync unknown';
  if (!status.enabled) return 'sync disabled';
  if (isOffline(status)) return 'sync offline';
  if (isAtRisk(status)) return 'pushes failing · claims at risk';
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
  if (isAtRisk(status)) {
    return { label: 'Claims at risk', color: 'var(--red)' };
  }
  if (status?.last_sync_error) {
    return { label: 'Sync degraded', color: 'var(--red)' };
  }
  if (!status?.enabled) {
    return { label: 'Sync disabled', color: 'var(--grey1)' };
  }
  const hidden = status?.hidden_projects?.length ?? 0;
  if (hidden > 0) {
    return { label: `${hidden} hidden project${hidden === 1 ? '' : 's'}`, color: 'var(--yellow)' };
  }
  const unpushed = status?.unpushed_commits ?? 0;
  if (unpushed > 0) {
    return { label: `${unpushed} unpushed`, color: 'var(--yellow)' };
  }
  return { label: 'All systems operational', color: 'var(--green)' };
}

// severity ranks a repo's state so the strip shows the repo that most needs
// attention. A repo without a remote ranks below a healthy one: a private
// repo next to a healthy shared repo must not read as "sync disabled".
function severity(s: SyncStatus): number {
  if (isOffline(s)) return 6;
  if (isAtRisk(s)) return 5;
  if (s.last_sync_error) return 4;
  if (s.syncing) return 3;
  if ((s.unpushed_commits ?? 0) > 0 || (s.hidden_projects?.length ?? 0) > 0) return 2;
  if (!s.enabled) return 0;
  return 1;
}

// eslint-disable-next-line react-refresh/only-export-components
export function pickWorst(statuses: SyncStatus[]): SyncStatus | null {
  return statuses.reduce<SyncStatus | null>(
    (worst, s) => (worst === null || severity(s) > severity(worst) ? s : worst),
    null,
  );
}

export function FootStrip({ version, syncStatuses }: FootStripProps) {
  const syncStatus = pickWorst(syncStatuses);
  const named = syncStatuses.length > 1 && syncStatus?.repo ? `${syncStatus.repo} · ` : '';
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
        {version ? `v${formatVersionWithLocalTime(version)}` : 'dev'} · {named}{syncLine(syncStatus)}
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
