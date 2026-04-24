import type { ActiveAgent } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';

interface ActiveAgentsFeedProps {
  agents: ActiveAgent[];
}

export function ActiveAgentsFeed({ agents }: ActiveAgentsFeedProps) {
  return (
    <div className="rounded-lg p-4" style={{ backgroundColor: 'var(--bg1)' }}>
      <h3 className="section-eyebrow mb-3">Active Agents</h3>
      {agents.length === 0 ? (
        <div className="text-sm py-4 text-center" style={{ color: 'var(--grey0)' }}>
          No agents currently active
        </div>
      ) : (
        <div className="space-y-2">
          {agents.map((agent) => (
            <div
              key={`${agent.agent_id}-${agent.card_id}`}
              className="flex items-center justify-between py-2 px-3 rounded"
              style={{ backgroundColor: 'var(--bg0)' }}
            >
              <div className="flex items-center gap-3 min-w-0">
                <span className="text-sm font-medium shrink-0" style={{ color: 'var(--aqua)' }}>
                  {agent.agent_id}
                </span>
                <span
                  className="truncate"
                  style={{
                    color: 'var(--aqua)',
                    fontFamily: 'var(--font-mono)',
                    fontWeight: 500,
                    fontSize: '11px',
                    letterSpacing: '0.04em',
                  }}
                >
                  {agent.card_id}
                </span>
                <span className="text-sm truncate" style={{ color: 'var(--fg)' }}>
                  {agent.card_title}
                </span>
              </div>
              <span className="text-xs shrink-0 ml-2" style={{ color: 'var(--grey0)' }}>
                {agent.last_heartbeat ? formatRelativeTime(agent.last_heartbeat) : 'no heartbeat'}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
