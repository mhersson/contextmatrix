import type { ActiveAgent } from '../../types';
import { avatarGradient } from '../../utils/colorHash';

export interface ActivityEntry {
  id: string;
  agent: string;
  action: string;
  cardId: string;
  ts: string;
}

interface NowRailProps {
  agents: ActiveAgent[];
  activityEntries: ActivityEntry[];
  maxAgents?: number;
  hasBackfill?: boolean;
}

function shortAgent(agentId: string): string {
  return agentId.replace(/^claude-/, '').replace(/^human:/, '');
}

function actionDotClass(action: string): string {
  switch (action) {
    case 'claim': return 'now-rail__act-dot--claim';
    case 'done':
    case 'shipped': return 'now-rail__act-dot--done';
    case 'review_requested':
    case 'review': return 'now-rail__act-dot--review';
    case 'system': return 'now-rail__act-dot--system';
    default: return '';
  }
}

function relativeTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const s = Math.round(ms / 1000);
  if (s < 5) return 'just now';
  if (s < 60) return `${s}s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.round(h / 24);
  return `${d}d`;
}

/**
 * Live agent list + capacity meter + activity feed. Phase 2 wired:
 * `maxAgents` shows the capacity section; `hasBackfill` switches the
 * activity label once the one-shot /activity backfill has loaded.
 */
const ACTIVITY_MAX = 8;

export function NowRail({ agents, activityEntries, maxAgents, hasBackfill }: NowRailProps) {
  const hasMax = maxAgents !== undefined && maxAgents > 0;
  const capacityPct = hasMax ? Math.min(100, Math.round((agents.length / maxAgents!) * 100)) : 0;
  const agentsCount = hasMax ? `${agents.length} / ${maxAgents}` : `${agents.length}`;
  const visibleActivity = activityEntries.slice(0, ACTIVITY_MAX);

  return (
    <aside className="now-rail">
      <div className="now-rail__section">
        <div className="now-rail__head">
          <span className="label">Now · agents</span>
          <span className="count">{agentsCount}</span>
        </div>
        {agents.length === 0 ? (
          <div className="now-rail__empty">No agents running.</div>
        ) : agents.map((a) => {
          const grad = avatarGradient(a.agent_id);
          return (
            <div className="now-rail__agent-row" key={a.agent_id}>
              <div
                className="agent-avatar agent-avatar--lg agent-avatar--online"
                style={{ '--av-from': grad.from, '--av-to': grad.to } as React.CSSProperties}
              />
              <div className="info">
                <span className="name">{shortAgent(a.agent_id)}</span>
                <span className="working">
                  on <span className="ref">{a.card_id}</span> · {a.card_title}
                </span>
              </div>
              <span className="elapsed">{a.since ? relativeTime(a.since) : ''}</span>
            </div>
          );
        })}
      </div>

      <div className="now-rail__section">
        <div className="now-rail__head">
          <span className="label">Capacity</span>
          <span className="count">{agents.length} running</span>
        </div>
        {hasMax ? (
          <>
            <div className="cap-bar">
              <div className="cap-fill" style={{ width: `${capacityPct}%` }} />
            </div>
            <div className="cap-meta">
              <span>{agents.length} / {maxAgents} agents</span>
              <span>{capacityPct}%</span>
            </div>
          </>
        ) : (
          <div className="cap-meta">
            <span>{agents.length} active · no cap set</span>
            <span>—</span>
          </div>
        )}
      </div>

      <div className="now-rail__section">
        <div className="now-rail__head">
          <span className="label">{hasBackfill ? 'Activity' : 'Activity · since page load'}</span>
          <span className="count">live</span>
        </div>
        {visibleActivity.length === 0 ? (
          <div className="now-rail__empty">No recent activity.</div>
        ) : visibleActivity.map((e) => (
          <div className="now-rail__act-row" key={e.id}>
            <div className={`now-rail__act-dot ${actionDotClass(e.action)}`} />
            <div className="now-rail__act-body">
              <span className="who">{shortAgent(e.agent)}</span> {e.action} <span className="ref">{e.cardId}</span>
            </div>
            <div className="now-rail__act-when">{relativeTime(e.ts)}</div>
          </div>
        ))}
      </div>
    </aside>
  );
}
