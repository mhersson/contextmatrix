import { Link } from 'react-router-dom';
import type { ProjectConfig } from '../../types';
import { useMemo } from 'react';
import {
  distributionSegments,
  formatUsd,
  projectRow,
} from './utils';
import type { DashboardData } from '../../types';

interface ProjectsTableProps {
  projects: ProjectConfig[];
  summaries: Map<string, DashboardData>;
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

export function ProjectsTable({ projects, summaries }: ProjectsTableProps) {
  const rows = useMemo(() => {
    const out = projects.map((p) => projectRow(p, summaries.get(p.name)));
    out.sort((a, b) => a.config.name.localeCompare(b.config.name));
    return out;
  }, [projects, summaries]);

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
          Sort: A→Z
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
                <th style={{ color: 'var(--grey1)' }} className="apd-num">
                  Cost
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const name = row.config.name;
                const display = row.config.display_name ?? name;
                const repo = row.config.repo ?? '';
                return (
                  <tr key={name} className="apd-project-row">
                    <td>
                      <Link
                        to={`/projects/${name}`}
                        className="apd-project-link"
                      >
                        <div className="flex items-center gap-3 min-w-0">
                          <span
                            className="apd-project-dot"
                            style={{
                              backgroundColor: 'var(--bg4)',
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
