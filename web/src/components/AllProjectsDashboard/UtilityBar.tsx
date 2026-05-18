import { useEffect, useState } from 'react';
import type { SyncStatus } from '../../types';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { formatRelativeTime } from '../CardPanel/utils';

interface UtilityBarProps {
  syncStatus: SyncStatus | null;
  version: string | null;
}

function localClock(now: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
}

function syncPillContent(status: SyncStatus | null): {
  label: string;
  dotColor: string;
  textColor: string;
  pulse: boolean;
} {
  if (!status || !status.enabled) {
    return {
      label: 'Sync disabled',
      dotColor: 'var(--grey0)',
      textColor: 'var(--grey1)',
      pulse: false,
    };
  }
  if (status.syncing) {
    return {
      label: 'Syncing…',
      dotColor: 'var(--aqua)',
      textColor: 'var(--grey2)',
      pulse: true,
    };
  }
  if (status.last_sync_error) {
    return {
      label: 'Sync error',
      dotColor: 'var(--red)',
      textColor: 'var(--red)',
      pulse: false,
    };
  }
  const label = status.last_sync_time
    ? `Synced ${formatRelativeTime(status.last_sync_time)}`
    : 'Not synced';
  return {
    label,
    dotColor: 'var(--green)',
    textColor: 'var(--grey2)',
    pulse: false,
  };
}

export function UtilityBar({ syncStatus, version }: UtilityBarProps) {
  const [now, setNow] = useState<Date>(() => new Date());
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(id);
  }, []);
  const { toggle } = useMobileSidebar();

  const pill = syncPillContent(syncStatus);

  return (
    <div className="apd-utility-bar">
      <div className="flex items-center gap-2 min-w-0">
        <button
          type="button"
          onClick={toggle}
          aria-label="Open menu"
          className="md:hidden inline-flex items-center justify-center p-1.5 border-0 bg-transparent cursor-pointer rounded transition-opacity hover:opacity-90 focus-visible:ring-2 focus-visible:ring-current focus-visible:outline-none"
          style={{ color: 'var(--grey2)' }}
        >
          <svg
            width={20}
            height={20}
            viewBox="0 0 20 20"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            aria-hidden="true"
          >
            <rect x="2" y="4" width="16" height="2" rx="1" fill="currentColor" />
            <rect x="2" y="9" width="16" height="2" rx="1" fill="currentColor" />
            <rect x="2" y="14" width="16" height="2" rx="1" fill="currentColor" />
          </svg>
        </button>
        <span className="hidden sm:inline" style={{ color: 'var(--grey1)' }}>
          ContextMatrix
        </span>
        <span className="hidden sm:inline" style={{ color: 'var(--bg4)' }}>
          /
        </span>
        <span className="truncate" style={{ color: 'var(--fg)' }}>
          Operations
        </span>
      </div>
      <div className="apd-util-right">
        <span
          className="apd-sync-pill"
          style={{
            color: pill.textColor,
            borderColor: 'var(--bg3)',
            backgroundColor: 'var(--bg1)',
          }}
          title={syncStatus?.last_sync_error || pill.label}
        >
          <span
            className={pill.pulse ? 'apd-pulse-dot' : 'apd-sync-pill-dot'}
            style={{ backgroundColor: pill.dotColor }}
          />
          <span className="apd-sync-pill-label">{pill.label}</span>
        </span>
        {version && (
          <span className="hidden md:inline" style={{ color: 'var(--grey1)' }}>
            Build <span style={{ color: 'var(--fg)', fontWeight: 500 }}>v{version}</span>
          </span>
        )}
        <span className="hidden md:inline" style={{ color: 'var(--grey1)' }}>
          {localClock(now)}
        </span>
      </div>
    </div>
  );
}
