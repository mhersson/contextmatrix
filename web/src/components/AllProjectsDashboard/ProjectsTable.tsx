import { Link, useNavigate } from 'react-router';
import type { ProjectConfig } from '../../types';
import { useMemo } from 'react';
import { chipTint } from '../../lib/chip';
import {
  distributionSegments,
  formatUsd,
  projectRow,
} from './utils';
import { boardsRepoName, type BoardsRepoInfo, type DashboardData } from '../../types';
import { DeckPanel } from './DeckPanel';

interface ProjectsTableProps {
  projects: ProjectConfig[];
  summaries: Map<string, DashboardData>;
  boardsRepos?: BoardsRepoInfo[];
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
    </div>
  );
}

export function ProjectsTable({ projects, summaries, boardsRepos = [] }: ProjectsTableProps) {
  const navigate = useNavigate();
  const rows = useMemo(() => {
    const out = projects.map((p) => projectRow(p, summaries.get(p.name)));
    out.sort((a, b) =>
      (a.config.display_name ?? a.config.name).localeCompare(
        b.config.display_name ?? b.config.name,
      ),
    );
    return out;
  }, [projects, summaries]);

  const renderRow = (row: ReturnType<typeof projectRow>) => {
    const name = row.config.name;
    const display = row.config.display_name ?? name;
    const repo = row.config.repo ?? '';
    return (
      <tr
        key={name}
        className="apd-project-row"
        style={{ cursor: 'pointer' }}
        onClick={() => navigate(`/projects/${name}`)}
      >
        <td>
          <Link
            to={`/projects/${name}`}
            className="apd-project-link"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-2.5 min-w-0">
              <span
                className="truncate min-w-0"
                style={{
                  color: 'var(--fg)',
                  fontWeight: 500,
                  fontSize: 13,
                  letterSpacing: '-0.01em',
                }}
                title={repo || undefined}
              >
                {display}
              </span>
              {row.config.prefix && (
                <span
                  className="chip-pill flex-shrink-0"
                  style={chipTint('var(--grey1)')}
                >
                  {row.config.prefix}
                </span>
              )}
            </div>
          </Link>
        </td>
        <td
          className="apd-num"
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11.5,
            color: 'var(--fg)',
          }}
        >
          {row.total}
        </td>
        <td>
          {row.data ? (
            <DistributionBar counts={row.data.state_counts} total={row.total} />
          ) : (
            <span style={{ color: 'var(--grey0)' }}> - </span>
          )}
        </td>
        <td
          className="apd-num"
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11.5,
            color: row.cost > 0 ? 'var(--yellow)' : 'var(--grey0)',
          }}
        >
          {formatUsd(row.cost)}
        </td>
      </tr>
    );
  };

  const meta =
    boardsRepos.length > 1 ? `${projects.length} · by repo` : `${projects.length} · A→Z`;

  return (
    <DeckPanel
      area="projects"
      accent="var(--blue)"
      title="Projects"
      meta={meta}
    >
      {rows.length === 0 ? (
        <div className="apd-panel-empty">No projects yet</div>
      ) : (
        <div className="apd-panel-body">
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
              {boardsRepos.length > 1
                ? boardsRepos.flatMap((repo) => {
                    const group = rows.filter((row) => boardsRepoName(row.config.boards_repo, boardsRepos) === repo.name);
                    if (group.length === 0) return [];
                    return [
                      <tr key={`repo-${repo.name}`} className="apd-group-row">
                        <td colSpan={4}>
                          <span className="sb-eyebrow">{repo.name}{repo.shared ? ' · shared' : ''}</span>
                        </td>
                      </tr>,
                      ...group.map(renderRow),
                    ];
                  })
                : rows.map(renderRow)}
            </tbody>
          </table>
        </div>
      )}
    </DeckPanel>
  );
}
