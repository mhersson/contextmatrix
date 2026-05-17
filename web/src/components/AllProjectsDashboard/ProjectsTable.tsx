import { Link } from 'react-router-dom';
import type { ActiveAgent, ProjectConfig } from '../../types';
import { useMemo } from 'react';
import {
  agentInitials,
  distributionSegments,
  formatUsd,
  isHumanAgent,
  projectRow,
  type ProjectRow,
} from './utils';
import type { DashboardData } from '../../types';

interface ProjectsTableProps {
  projects: ProjectConfig[];
  summaries: Map<string, DashboardData>;
  errors: Set<string>;
}

const STATUS_LABEL: Record<ProjectRow['status'], string> = {
  on_track: 'On track',
  attention: 'Attention',
  stalled: 'Stalled',
  idle: 'Idle',
  unavailable: 'Unavailable',
};

const STATUS_COLORS: Record<
  ProjectRow['status'],
  { bg: string; fg: string; dot: string }
> = {
  on_track: { bg: 'var(--bg-green)', fg: 'var(--green)', dot: 'var(--green)' },
  attention: { bg: 'var(--bg-yellow)', fg: 'var(--yellow)', dot: 'var(--yellow)' },
  stalled: { bg: 'var(--bg-red)', fg: 'var(--red)', dot: 'var(--red)' },
  idle: { bg: 'var(--bg2)', fg: 'var(--grey1)', dot: 'var(--bg4)' },
  unavailable: { bg: 'var(--bg-red)', fg: 'var(--red)', dot: 'var(--red)' },
};

const MAX_AVATARS = 3;

function AvatarStack({ agents }: { agents: ActiveAgent[] }) {
  const unique: ActiveAgent[] = [];
  const seen = new Set<string>();
  for (const a of agents) {
    if (seen.has(a.agent_id)) continue;
    seen.add(a.agent_id);
    unique.push(a);
  }
  const shown = unique.slice(0, MAX_AVATARS);
  const overflow = unique.length - shown.length;
  if (unique.length === 0) {
    return <span style={{ color: 'var(--grey0)', fontSize: 12 }}>—</span>;
  }
  return (
    <div className="flex items-center">
      <div className="flex items-center">
        {shown.map((a, idx) => {
          const human = isHumanAgent(a.agent_id);
          return (
            <span
              key={a.agent_id}
              className="apd-avatar"
              title={a.agent_id}
              style={{
                marginLeft: idx === 0 ? 0 : -7,
                backgroundColor: human ? 'var(--bg-blue)' : 'var(--bg-aqua)',
                color: human ? 'var(--blue)' : 'var(--aqua)',
                borderColor: 'var(--bg1)',
              }}
            >
              {agentInitials(a.agent_id)}
            </span>
          );
        })}
      </div>
      {overflow > 0 && (
        <span
          className="apd-avatar-more"
          style={{
            backgroundColor: 'var(--bg2)',
            color: 'var(--grey1)',
          }}
        >
          +{overflow}
        </span>
      )}
    </div>
  );
}

function DistributionBar({
  counts,
  total,
}: {
  counts: Record<string, number>;
  total: number;
}) {
  const segments = distributionSegments(counts);
  return (
    <div className="apd-dist-row">
      <div
        className="apd-dist-bar"
        style={{ backgroundColor: 'var(--bg2)' }}
        aria-label={`${total} cards, distribution`}
        title={segments.map((s) => `${s.state}: ${s.count}`).join(' · ')}
      >
        {segments.map((s) => (
          <span
            key={s.state}
            style={{ flex: s.count, backgroundColor: s.color }}
            aria-hidden="true"
          />
        ))}
      </div>
      <span className="apd-dist-num" style={{ color: 'var(--grey1)' }}>
        {total}
      </span>
    </div>
  );
}

function StatusPill({ status }: { status: ProjectRow['status'] }) {
  const c = STATUS_COLORS[status];
  return (
    <span
      className="apd-status-pill"
      style={{ backgroundColor: c.bg, color: c.fg }}
    >
      <span
        aria-hidden="true"
        className="apd-status-pill-dot"
        style={{ backgroundColor: c.dot }}
      />
      {STATUS_LABEL[status]}
    </span>
  );
}

export function ProjectsTable({ projects, summaries, errors }: ProjectsTableProps) {
  const rows = useMemo(() => {
    const out = projects.map((p) => projectRow(p, summaries.get(p.name), errors.has(p.name)));
    out.sort((a, b) => b.total - a.total);
    return out;
  }, [projects, summaries, errors]);

  return (
    <section
      className="apd-card"
      style={{
        borderColor: 'var(--bg3)',
        backgroundColor: 'var(--bg1)',
        overflow: 'hidden',
      }}
    >
      <div
        className="flex items-center justify-between"
        style={{
          padding: '16px 20px 14px',
          borderBottom: '1px solid var(--bg2)',
        }}
      >
        <div className="flex items-baseline gap-2.5">
          <h2 className="apd-section-title">Projects</h2>
          <span className="apd-count" style={{ color: 'var(--grey1)' }}>
            {projects.length}
          </span>
        </div>
        <span
          style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--grey1)' }}
        >
          Sort: cards ↓
        </span>
      </div>
      {rows.length === 0 ? (
        <div
          style={{
            padding: '32px 20px',
            textAlign: 'center',
            color: 'var(--grey0)',
            fontSize: 13,
          }}
        >
          No projects yet
        </div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="apd-projects-table" style={{ color: 'var(--fg)' }}>
            <thead>
              <tr>
                <th style={{ color: 'var(--grey1)' }}>Project</th>
                <th style={{ color: 'var(--grey1)' }} className="apd-num">
                  Cards
                </th>
                <th style={{ color: 'var(--grey1)' }}>Distribution</th>
                <th style={{ color: 'var(--grey1)' }}>Active agents</th>
                <th style={{ color: 'var(--grey1)' }} className="apd-num">
                  Cost
                </th>
                <th style={{ color: 'var(--grey1)' }}>Status</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const name = row.config.name;
                const display = row.config.display_name ?? name;
                const repo = row.config.repo ?? '';
                const agents = row.data?.active_agents ?? [];
                return (
                  <tr key={name} className="apd-project-row">
                    <td>
                      <Link
                        to={`/projects/${name}/dashboard`}
                        className="apd-project-link"
                      >
                        <div className="flex items-center gap-3 min-w-0">
                          <span
                            className="apd-project-dot"
                            style={{
                              backgroundColor: STATUS_COLORS[row.status].dot,
                            }}
                          />
                          <span className="min-w-0">
                            <span
                              className="block truncate"
                              style={{
                                color: 'var(--fg)',
                                fontWeight: 500,
                                fontSize: 13.5,
                                letterSpacing: '-0.01em',
                              }}
                            >
                              {display}
                            </span>
                            <span
                              className="block truncate"
                              style={{
                                fontFamily: 'var(--font-mono)',
                                fontSize: 11,
                                color: 'var(--grey1)',
                                letterSpacing: '-0.01em',
                                marginTop: 1,
                              }}
                            >
                              {repo || name}
                            </span>
                          </span>
                        </div>
                      </Link>
                    </td>
                    <td
                      className="apd-num"
                      style={{
                        fontFamily: 'var(--font-mono)',
                        fontSize: 12,
                        color: 'var(--fg)',
                      }}
                    >
                      {row.total}
                    </td>
                    <td>
                      {row.data ? (
                        <DistributionBar counts={row.data.state_counts} total={row.total} />
                      ) : (
                        <span style={{ color: 'var(--grey0)' }}>—</span>
                      )}
                    </td>
                    <td>
                      <AvatarStack agents={agents} />
                    </td>
                    <td
                      className="apd-num"
                      style={{
                        fontFamily: 'var(--font-mono)',
                        fontSize: 12,
                        color: row.cost > 0 ? 'var(--yellow)' : 'var(--grey0)',
                      }}
                    >
                      {formatUsd(row.cost)}
                    </td>
                    <td>
                      <StatusPill status={row.status} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
