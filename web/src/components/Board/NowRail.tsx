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

/**
 * Live agent list + activity feed. Phase 1 shows only events accumulated
 * since the page was opened (no backfill — that endpoint arrives in Phase 2).
 */
export function NowRail({ agents, activityEntries }: NowRailProps) {
  return (
    <aside className="now-rail">
      <div className="now-rail__section">
        <div className="now-rail__head">
          <span className="label">Now · agents</span>
          <span className="count">{agents.length}</span>
        </div>
        {agents.map((a) => {
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
              <span className="elapsed" />
            </div>
          );
        })}
      </div>

      <div className="now-rail__section">
        <div className="now-rail__head">
          <span className="label">Activity · since page load</span>
          <span className="count">live</span>
        </div>
        {activityEntries.map((e) => (
          <div className="now-rail__act-row" key={e.id}>
            <div className={`now-rail__act-dot ${actionDotClass(e.action)}`} />
            <div className="now-rail__act-body">
              <span className="who">{e.agent}</span> {e.action} <span className="ref">{e.cardId}</span>
            </div>
            <div className="now-rail__act-when">{e.ts}</div>
          </div>
        ))}
      </div>
    </aside>
  );
}
