import { useEffect, useState } from 'react';
import type { SyncStatus } from '../../types';

interface BoardFooterProps {
  syncStatus?: SyncStatus | null;
  connected?: boolean;
  cardCount: number;
  columnCount: number;
  nowRailOpen?: boolean;
  onToggleNowRail?: () => void;
  onSyncClick?: () => void;
}

function relativeTime(iso: string, nowMs: number): string {
  const ms = nowMs - new Date(iso).getTime();
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  return `${h}h ago`;
}

export function BoardFooter({
  syncStatus,
  connected,
  cardCount,
  columnCount,
  nowRailOpen,
  onToggleNowRail,
  onSyncClick,
}: BoardFooterProps) {
  // Local 30s tick so the "Xs ago" portion of the sync label refreshes even
  // when the parent doesn't re-render. Only runs while we have a sync time
  // to format - if there isn't one, there's nothing to refresh.
  const [now, setNow] = useState(() => Date.now());
  const lastSyncTime = syncStatus?.last_sync_time ?? null;
  useEffect(() => {
    if (!lastSyncTime) return;
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, [lastSyncTime]);

  const syncing = syncStatus?.syncing === true;
  const syncError = syncStatus?.last_sync_error && syncStatus.last_sync_error.length > 0
    ? syncStatus.last_sync_error
    : null;

  // Shared-repo states: offline (remote unreachable) and unpushed local
  // commits waiting to go out. Both are absent (undefined) for non-shared
  // repos, so they never engage there.
  const offline = syncStatus?.shared === true && syncStatus.remote_reachable === false;
  const unpushed = syncStatus?.unpushed_commits ?? 0;
  const resolutions = syncStatus?.resolutions?.length ?? 0;

  // Compute the label from syncStatus. When no status is supplied (rare -
  // tests / detached usage), fall back to a static idle label.
  let computedLabel: string;
  if (syncing) {
    computedLabel = 'Syncing…';
  } else if (offline) {
    computedLabel = 'git sync · offline';
  } else if (syncStatus) {
    computedLabel = lastSyncTime
      ? `git sync · ${relativeTime(lastSyncTime, now)}`
      : 'git sync · idle';
    if (unpushed > 0) {
      computedLabel += ` · ${unpushed} unpushed`;
    }
  } else {
    computedLabel = 'git sync · idle';
  }

  const labelStyle: React.CSSProperties = offline || syncError
    ? { color: 'var(--red)' }
    : {};

  const titleLines: string[] = [];
  if (offline && syncStatus?.last_remote_error) {
    titleLines.push(syncStatus.last_remote_error);
  }
  if (syncError) {
    titleLines.push(syncError);
  }
  if (resolutions > 0) {
    titleLines.push(`${resolutions} merge resolution${resolutions === 1 ? '' : 's'} in the last syncs`);
  }
  titleLines.push(offline || syncError ? 'Click to retry' : 'Click to sync now');
  const labelTitle = titleLines.join('\n');

  // Dot rendered to the left of the label when syncing, offline, in error,
  // or waiting to push local commits.
  const dotClass = syncing
    ? 'board-footer__sync-dot--aqua'
    : offline || syncError
      ? 'board-footer__sync-dot--red'
      : unpushed > 0
        ? 'board-footer__sync-dot--amber'
        : null;
  const dot = dotClass ? (
    <span
      className={`board-footer__sync-dot ${dotClass}`}
      aria-hidden="true"
    />
  ) : null;

  // While syncing we render the label as a span (with aria-busy) rather
  // than a disabled button - keeps the markup honest and avoids the
  // "looks-like-a-button-but-isn't" disabled state.
  const renderSyncContent = () => {
    if (syncing) {
      return (
        <span
          className="board-footer__sync-label board-footer__sync-label--syncing"
          aria-busy="true"
          aria-live="polite"
        >
          {dot}
          {computedLabel}
        </span>
      );
    }
    if (onSyncClick) {
      return (
        <button
          type="button"
          onClick={onSyncClick}
          className="board-footer__sync-label"
          title={labelTitle}
          style={labelStyle}
        >
          {dot}
          {computedLabel}
        </button>
      );
    }
    return (
      <span title={labelTitle} style={labelStyle}>
        {dot}
        {computedLabel}
      </span>
    );
  };

  return (
    <div className="board-footer">
      <div className="board-footer__left">
        {connected !== undefined && (
          <span
            className={
              'board-footer__conn ' +
              (connected ? 'board-footer__conn--ok' : 'board-footer__conn--off')
            }
            title={connected ? 'Live updates connected' : 'Server connection lost'}
          >
            <span
              className={
                'board-footer__sync-dot ' +
                (connected ? 'board-footer__sync-dot--green' : 'board-footer__sync-dot--red')
              }
              aria-hidden="true"
            />
            <span className="board-footer__conn-label">
              {connected ? 'online' : 'offline'}
            </span>
          </span>
        )}
        {renderSyncContent()}
      </div>
      <span>{cardCount} cards · {columnCount} columns</span>
      {onToggleNowRail ? (
        <button
          type="button"
          onClick={onToggleNowRail}
          className="board-footer__rail-toggle"
          aria-pressed={nowRailOpen}
          title={nowRailOpen ? 'Hide right rail' : 'Show right rail'}
        >
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
            <rect x="3" y="4" width="18" height="16" rx="2" />
            <line x1="15" y1="4" x2="15" y2="20" />
          </svg>
          <span>{nowRailOpen ? 'Hide rail' : 'Show rail'}</span>
        </button>
      ) : (
        <span />
      )}
    </div>
  );
}
