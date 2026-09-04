import type { SyncStatus } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';

/** describeSync turns a repo's sync status into the dot colour and its tooltip. */
// eslint-disable-next-line react-refresh/only-export-components
export function describeSync(status: SyncStatus | null): { color: string; title: string } {
  if (!status) return { color: 'var(--grey0)', title: 'sync unknown' };
  if (!status.enabled) return { color: 'var(--grey0)', title: 'sync disabled (no remote)' };

  const lines: string[] = [];
  let color = 'var(--green)';

  if (status.shared && status.remote_reachable === false) {
    color = 'var(--red)';
    lines.push('offline: create and claim are disabled');
  } else if (status.shared && status.claims_at_risk) {
    color = 'var(--red)';
    lines.push('pushes failing: claims at risk');
  } else if (status.last_sync_error) {
    color = 'var(--red)';
    lines.push(`sync error: ${status.last_sync_error}`);
  } else if (status.syncing) {
    color = 'var(--aqua)';
    lines.push('syncing');
  }

  const unpushed = status.unpushed_commits ?? 0;
  if (unpushed > 0) {
    if (color === 'var(--green)') color = 'var(--yellow)';
    lines.push(`${unpushed} unpushed commit${unpushed === 1 ? '' : 's'}`);
  }

  const hidden = status.hidden_projects ?? [];
  if (hidden.length > 0) {
    if (color === 'var(--green)') color = 'var(--yellow)';
    lines.push(`hidden: ${hidden.join(', ')} (name owned by an earlier boards repo)`);
  }

  const resolved = status.resolutions?.length ?? 0;
  if (resolved > 0) {
    lines.push(`resolved ${resolved} conflict${resolved === 1 ? '' : 's'}`);
  }

  if (lines.length === 0) {
    lines.push(status.last_sync_time ? `healthy · synced ${formatRelativeTime(status.last_sync_time)}` : 'healthy');
  }

  return { color, title: lines.join('\n') };
}

interface SyncDotProps {
  status: SyncStatus | null;
  repo: string;
}

export function SyncDot({ status, repo }: SyncDotProps) {
  const { color, title } = describeSync(status);
  return (
    <span
      role="img"
      aria-label={`${repo} sync: ${title}`}
      title={title}
      className="sb-sync-dot"
      style={{ backgroundColor: color }}
    />
  );
}
