import { Link } from 'react-router';
import type { ActiveAgent } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { DeckPanel } from './DeckPanel';
import {
  agentInitials,
  compactSeconds,
  isHumanAgent,
  medianHeartbeatSeconds,
  oldestClaim,
  projectForCardId,
} from './utils';

const cardIdStyle = {
  fontFamily: 'var(--font-mono)',
  fontSize: 10.5,
  fontWeight: 500,
  letterSpacing: '0.04em',
  color: 'var(--grey1)',
} as const;

function FooterCell({
  label,
  value,
  valueColor,
}: {
  label: string;
  value: string;
  valueColor?: string;
}) {
  return (
    <div>
      <div style={{ fontSize: 9.5, color: 'var(--grey0)', fontWeight: 500 }}>{label}</div>
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontWeight: 500,
          fontSize: 15,
          color: valueColor ?? 'var(--fg)',
          marginTop: 2,
          fontVariantNumeric: 'tabular-nums',
          letterSpacing: '-0.015em',
        }}
      >
        {value}
      </div>
    </div>
  );
}

export function AgentsOnDuty({
  activeAgents,
  stalledCount,
  prefixMap,
}: {
  activeAgents: ActiveAgent[];
  stalledCount: number;
  prefixMap: Map<string, string>;
}) {
  const median = medianHeartbeatSeconds(activeAgents);
  const oldest = oldestClaim(activeAgents);

  return (
    <DeckPanel
      area="agents"
      accent="var(--aqua)"
      title="Agents on duty"
      meta={`${activeAgents.length} live`}
    >
      {activeAgents.length === 0 ? (
        <div className="apd-panel-empty" style={{ flex: 1 }}>
          No agents currently active
        </div>
      ) : (
        <div className="apd-panel-body" style={{ padding: '0 10px 8px' }}>
          {activeAgents.map((a) => {
            const human = isHumanAgent(a.agent_id);
            const project = projectForCardId(a.card_id, prefixMap);
            const lastBeat = a.last_heartbeat
              ? formatRelativeTime(a.last_heartbeat)
              : 'no beat';
            const inner = (
              <>
                <span
                  className="apd-agent-avatar"
                  style={{
                    backgroundColor: human ? 'var(--bg-blue)' : 'var(--bg-aqua)',
                    color: human ? 'var(--blue)' : 'var(--aqua)',
                  }}
                  aria-hidden="true"
                >
                  {agentInitials(a.agent_id)}
                </span>
                <span className="min-w-0">
                  <span
                    className="flex items-center gap-1.5"
                    style={{
                      fontSize: 12.5,
                      color: 'var(--fg)',
                      fontWeight: 500,
                      letterSpacing: '-0.01em',
                    }}
                  >
                    <span className="truncate">{a.agent_id}</span>
                    <span
                      className="apd-role-tag"
                      style={{
                        color: 'var(--grey1)',
                        backgroundColor: 'var(--bg2)',
                        border: '1px solid var(--bg3)',
                      }}
                    >
                      {human ? 'Human' : 'AI'}
                    </span>
                  </span>
                  <span
                    className="block truncate"
                    style={{ fontSize: 11.5, color: 'var(--grey1)', marginTop: 2 }}
                  >
                    <span style={cardIdStyle}>{a.card_id}</span> · {a.card_title}
                  </span>
                </span>
                <span
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 10.5,
                    color: 'var(--grey1)',
                    textAlign: 'right',
                    whiteSpace: 'nowrap',
                  }}
                >
                  <span style={{ display: 'block', color: 'var(--grey0)', fontSize: 9.5 }}>
                    last beat
                  </span>
                  {lastBeat}
                </span>
              </>
            );
            return project ? (
              <Link
                key={`${a.agent_id}-${a.card_id}`}
                to={`/projects/${project}`}
                className="apd-agent-row"
              >
                {inner}
              </Link>
            ) : (
              <div
                key={`${a.agent_id}-${a.card_id}`}
                className="apd-agent-row apd-agent-row-static"
              >
                {inner}
              </div>
            );
          })}
        </div>
      )}
      <div className="apd-agents-footer">
        <FooterCell
          label="Median heartbeat"
          value={median !== null ? compactSeconds(median) : ' - '}
        />
        <FooterCell label="Oldest claim" value={oldest} />
        <FooterCell
          label="Stalled"
          value={String(stalledCount)}
          valueColor={stalledCount > 0 ? 'var(--red)' : 'var(--fg)'}
        />
      </div>
    </DeckPanel>
  );
}
