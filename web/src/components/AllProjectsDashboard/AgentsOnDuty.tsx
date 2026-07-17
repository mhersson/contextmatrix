import { Link } from 'react-router-dom';
import type { ActiveAgent } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import {
  agentInitials,
  compactSeconds,
  isHumanAgent,
  medianHeartbeatSeconds,
  oldestClaim,
  projectForCardId,
} from './utils';

const agentRowStyle = {
  display: 'grid',
  gridTemplateColumns: 'auto 1fr auto',
  gap: 12,
  alignItems: 'center',
  padding: '11px 12px',
  borderRadius: 5,
  textAlign: 'left' as const,
  textDecoration: 'none',
};

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
    <div style={{ backgroundColor: 'var(--bg1)', padding: '13px 16px' }}>
      <div style={{ fontSize: 10.5, color: 'var(--grey1)', fontWeight: 500 }}>{label}</div>
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontWeight: 500,
          fontSize: 20,
          color: valueColor ?? 'var(--fg)',
          marginTop: 3,
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
    <>
      {activeAgents.length === 0 ? (
        <div
          style={{
            padding: '32px 20px',
            textAlign: 'center',
            fontSize: 12.5,
            color: 'var(--grey0)',
            fontStyle: 'italic',
          }}
        >
          No agents currently active
        </div>
      ) : (
        <div style={{ padding: 8 }}>
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
                      fontSize: 13,
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
                    className="block truncate apd-meta-line"
                    style={{
                      letterSpacing: '-0.01em',
                      marginTop: 2,
                    }}
                  >
                    <span style={{ color: 'var(--aqua)', fontWeight: 500 }}>
                      {a.card_id}
                    </span>{' '}
                    · {a.card_title}
                  </span>
                </span>
                <span
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 11,
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
                style={agentRowStyle}
              >
                {inner}
              </Link>
            ) : (
              <div
                key={`${a.agent_id}-${a.card_id}`}
                className="apd-agent-row apd-agent-row-static"
                style={agentRowStyle}
              >
                {inner}
              </div>
            );
          })}
        </div>
      )}
      <div
        className="apd-agents-footer"
        style={{ borderTop: '1px solid var(--bg2)' }}
      >
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
    </>
  );
}
