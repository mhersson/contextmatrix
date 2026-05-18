import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import type { CardCost } from '../../types';
import { filterCardCosts } from '../../utils/costTableUtils';
import { projectForCardId } from './utils';

interface TopCardsPanelProps {
  cardCosts: CardCost[];
  prefixMap: Map<string, string>;
}

const TOP_N = 5;

export function TopCardsPanel({ cardCosts, prefixMap }: TopCardsPanelProps) {
  const [search, setSearch] = useState('');

  const sorted = useMemo(
    () => [...cardCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd),
    [cardCosts],
  );
  const filtered = useMemo(() => filterCardCosts(sorted, search), [sorted, search]);
  const top = filtered.slice(0, TOP_N);
  const q = search.trim();

  const footerLabel = q
    ? `Top ${top.length} of ${filtered.length} matching · ${sorted.length} total`
    : `Top ${top.length} of ${sorted.length} cards`;
  const headBadge = q
    ? `${top.length} / ${filtered.length}`
    : `${top.length} / ${sorted.length}`;

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
        style={{
          padding: '12px 16px 10px',
          borderBottom: '1px solid var(--bg2)',
        }}
      >
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
            {q ? 'No matching cards' : 'No card costs reported yet'}
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
