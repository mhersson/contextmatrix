import type { ActivityEntry } from '../../types';
import { formatRelativeTime } from './utils';

interface CardPanelActivityProps {
  activityLog: ActivityEntry[] | undefined;
}

/**
 * Activity log inside the Automation rail tab. Mirrors the design mock's
 * `.bf-act` grid (`/tmp/card-panel-explorer.html:1975-1990`):
 *
 *   ●   <agent>          <relative-time>
 *       <action> — <message>
 *
 * Dot color reflects the action kind (claim, transition/progress, complete,
 * error). Outer wrapper is `.bf-auto-activity` with sticky header.
 */
export function CardPanelActivity({ activityLog }: CardPanelActivityProps) {
  const entries = [...(activityLog || [])].reverse();

  return (
    <div className="bf-auto-activity">
      <div className="bf-auto-activity-head">
        <h4 className="section-eyebrow">
          Activity
          <span className="font-mono normal-case ml-2" style={{ color: 'var(--grey0)', fontWeight: 400, letterSpacing: '0.04em' }}>
            · {entries.length}
          </span>
        </h4>
      </div>
      <div className="bf-auto-activity-scroll">
        {entries.length === 0 ? (
          <div className="text-xs italic" style={{ color: 'var(--grey1)', padding: '8px 0' }}>
            No activity yet.
          </div>
        ) : (
          entries.map((entry) => {
            const dotKind = dotKindForAction(entry.action);
            // Activity log is append-only (per data-model.md #6), so the
            // (ts, agent, action) tuple uniquely identifies each entry. Using
            // idx breaks when reverse() shifts indices on every new append.
            const key = `${entry.ts}:${entry.agent}:${entry.action}`;
            return (
              <div key={key} className="bf-act">
                <div className={`bf-act-dot${dotKind ? ` bf-act-dot-${dotKind}` : ''}`} />
                <div className="bf-act-body">
                  <div className="bf-act-agent">{entry.agent}</div>
                  <div>
                    <span style={{ color: 'var(--fg)' }}>{entry.action}</span>
                    {entry.message && (
                      <>
                        <span style={{ color: 'var(--grey1)' }}> — </span>
                        <span>{entry.message}</span>
                      </>
                    )}
                  </div>
                </div>
                <div className="bf-act-time">{formatRelativeTime(entry.ts)}</div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

type DotKind = 'done' | 'progress' | 'claim' | 'error' | null;

function dotKindForAction(action: string): DotKind {
  const a = action.toLowerCase();
  if (a.includes('claim')) return 'claim';
  if (a.includes('release')) return 'progress';
  if (a.includes('done') || a.includes('complete')) return 'done';
  if (a.includes('fail') || a.includes('error') || a.includes('stall')) return 'error';
  if (a.includes('state') || a.includes('transition') || a.includes('progress')) return 'progress';
  return null;
}
