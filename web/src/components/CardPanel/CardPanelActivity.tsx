import type { ActivityEntry } from '../../types';
import { formatRelativeTime } from './utils';

interface CardPanelActivityProps {
  activityLog: ActivityEntry[] | undefined;
}

export function CardPanelActivity({ activityLog }: CardPanelActivityProps) {
  const entries = [...(activityLog || [])].reverse();

  if (entries.length === 0) return null;

  return (
    <div>
      <label className="block text-xs text-[var(--grey1)] mb-2">Activity Log</label>
      <div className="space-y-2 max-h-[200px] overflow-y-auto">
        {entries.map((entry, idx) => (
          <div
            key={idx}
            className="p-2 rounded bg-[var(--bg0)] border border-[var(--bg3)] text-sm"
          >
            <div className="flex items-center gap-2 text-xs text-[var(--grey1)] mb-1">
              <span className="text-[var(--aqua)]">{entry.agent}</span>
              <span>·</span>
              <span>{formatRelativeTime(entry.ts)}</span>
              <span>·</span>
              <span className="text-[var(--purple)]">{entry.action}</span>
            </div>
            <p className="text-[var(--fg)]">{entry.message}</p>
          </div>
        ))}
      </div>
    </div>
  );
}
