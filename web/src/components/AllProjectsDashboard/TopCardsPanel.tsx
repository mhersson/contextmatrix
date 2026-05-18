import { useCallback, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import type { CardCost, ProjectConfig } from '../../types';
import { filterCardCosts } from '../../utils/costTableUtils';
import { projectForCardId } from './utils';

interface TopCardsPanelProps {
  cardCosts: CardCost[];
  prefixMap: Map<string, string>;
  projects: ProjectConfig[];
}

const TOP_N = 5;

export function TopCardsPanel({ cardCosts, prefixMap, projects }: TopCardsPanelProps) {
  const [search, setSearch] = useState('');
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedProject = searchParams.get('project') ?? '';

  const handleProjectChange = useCallback(
    (next: string) => {
      const params = new URLSearchParams(searchParams);
      if (next) params.set('project', next);
      else params.delete('project');
      setSearchParams(params, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const projectOptions = useMemo(
    () =>
      [...projects].sort((a, b) =>
        (a.display_name ?? a.name).localeCompare(b.display_name ?? b.name),
      ),
    [projects],
  );

  const selectedLabel = useMemo(() => {
    if (!selectedProject) return '';
    const p = projects.find((x) => x.name === selectedProject);
    return p?.display_name ?? selectedProject;
  }, [selectedProject, projects]);

  const sorted = useMemo(
    () => [...cardCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd),
    [cardCosts],
  );
  const projectFiltered = useMemo(
    () =>
      selectedProject
        ? sorted.filter((c) => projectForCardId(c.card_id, prefixMap) === selectedProject)
        : sorted,
    [sorted, selectedProject, prefixMap],
  );
  const filtered = useMemo(
    () => filterCardCosts(projectFiltered, search),
    [projectFiltered, search],
  );
  const top = filtered.slice(0, TOP_N);
  const q = search.trim();

  const footerLabel = q
    ? `Top ${top.length} of ${filtered.length} matching · ${sorted.length} total`
    : selectedProject
      ? `Top ${top.length} of ${filtered.length} in ${selectedLabel} · ${sorted.length} total`
      : `Top ${top.length} of ${sorted.length} cards`;
  const headBadge = q
    ? `${top.length} / ${filtered.length}`
    : selectedProject
      ? `${top.length} / ${filtered.length}`
      : `${top.length} / ${sorted.length}`;

  const emptyCopy = q
    ? 'No matching cards'
    : selectedProject
      ? `No cards in ${selectedLabel} yet`
      : 'No card costs reported yet';

  const rowStyle = {
    display: 'grid',
    gridTemplateColumns: 'auto 1fr auto',
    gap: 12,
    alignItems: 'center',
    padding: '10px 12px',
    borderRadius: 5,
    textAlign: 'left' as const,
    textDecoration: 'none',
  };

  return (
    <section
      className="apd-card"
      style={{
        borderColor: 'var(--bg3)',
        backgroundColor: 'var(--bg1)',
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        className="flex items-baseline gap-2.5"
        style={{
          padding: '16px 20px 14px',
          borderBottom: '1px solid var(--bg2)',
        }}
      >
        <h2 className="apd-section-title">Top cards</h2>
        <span
          className="apd-count"
          style={{ color: 'var(--grey1)', fontFamily: 'var(--font-mono)' }}
        >
          {headBadge}
        </span>
      </div>
      <div
        className="flex items-center gap-2"
        style={{
          padding: '12px 16px 10px',
          borderBottom: '1px solid var(--bg2)',
        }}
      >
        <label className="sr-only" htmlFor="topcards-project-filter">
          Project
        </label>
        <select
          id="topcards-project-filter"
          aria-label="Project"
          value={selectedProject}
          onChange={(e) => handleProjectChange(e.target.value)}
          className="apd-search-input"
          style={{
            backgroundColor: 'var(--bg2)',
            color: 'var(--fg)',
            border: '1px solid var(--bg3)',
            flexShrink: 0,
          }}
        >
          <option value="">All projects</option>
          {projectOptions.map((p) => (
            <option key={p.name} value={p.name}>
              {p.display_name ?? p.name}
            </option>
          ))}
        </select>
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search by card ID…"
          spellCheck={false}
          autoComplete="off"
          className="apd-search-input"
          style={{
            backgroundColor: 'var(--bg2)',
            color: 'var(--fg)',
            border: '1px solid var(--bg3)',
            flex: 1,
            minWidth: 0,
          }}
        />
      </div>
      <div style={{ padding: '6px 6px 8px', flex: 1, minHeight: 0 }}>
        {top.length === 0 ? (
          <div
            style={{
              padding: '32px 16px',
              textAlign: 'center',
              fontSize: 12.5,
              color: 'var(--grey0)',
              fontStyle: 'italic',
            }}
          >
            {emptyCopy}
          </div>
        ) : (
          top.map((c) => {
            const project = projectForCardId(c.card_id, prefixMap);
            const cost = `$${c.estimated_cost_usd.toFixed(2)}`;
            const idCell = (
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 11.5,
                  color: 'var(--aqua)',
                  fontWeight: 500,
                  letterSpacing: '-0.01em',
                  whiteSpace: 'nowrap',
                }}
              >
                {c.card_id}
              </span>
            );
            const titleCell = (
              <span className="min-w-0">
                <span
                  className="block truncate"
                  style={{
                    fontSize: 13,
                    color: 'var(--fg)',
                    letterSpacing: '-0.01em',
                  }}
                >
                  {c.card_title}
                </span>
                <span
                  className="block truncate"
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 11,
                    color: c.assigned_agent ? 'var(--grey1)' : 'var(--grey0)',
                    fontStyle: c.assigned_agent ? 'normal' : 'italic',
                    letterSpacing: '-0.01em',
                    marginTop: 2,
                  }}
                >
                  {c.assigned_agent || 'unassigned'}
                </span>
              </span>
            );
            const costCell = (
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 12,
                  color: 'var(--yellow)',
                  fontVariantNumeric: 'tabular-nums',
                  letterSpacing: '-0.01em',
                  whiteSpace: 'nowrap',
                }}
              >
                {cost}
              </span>
            );
            return project ? (
              <Link
                key={c.card_id}
                to={`/projects/${encodeURIComponent(project)}?card=${encodeURIComponent(c.card_id)}`}
                className="apd-card-row"
                style={rowStyle}
              >
                {idCell}
                {titleCell}
                {costCell}
              </Link>
            ) : (
              <div
                key={c.card_id}
                className="apd-card-row apd-card-row-static"
                style={rowStyle}
              >
                {idCell}
                {titleCell}
                {costCell}
              </div>
            );
          })
        )}
      </div>
      <div
        className="flex items-center justify-between"
        style={{
          padding: '10px 16px',
          borderTop: '1px solid var(--bg2)',
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          color: 'var(--grey1)',
          letterSpacing: '-0.01em',
        }}
      >
        <span>{footerLabel}</span>
        <span style={{ color: 'var(--grey1)' }}>Sorted by cost ↓</span>
      </div>
    </section>
  );
}
