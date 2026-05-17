import type { CardFilter } from '../../types';

interface FilterChipBarProps {
  filter: CardFilter;
  currentAgent: string | null;
  onFilterChange: (filter: CardFilter) => void;
}

type StringFilterKey = Exclude<keyof CardFilter, 'vetted'>;

/**
 * Replaces the dropdown-style FilterBar with a chip-driven filter row.
 * The chips are visually prominent so the active filter is obvious at
 * a glance. Owns the search input + view toggle.
 */
export function FilterChipBar({ filter, currentAgent, onFilterChange }: FilterChipBarProps) {
  function toggle(key: StringFilterKey, value: string) {
    const next = { ...filter };
    if (filter[key] === value) {
      delete next[key];
    } else {
      next[key] = value;
    }
    onFilterChange(next);
  }

  const isActive = (key: StringFilterKey, value: string) => filter[key] === value;

  return (
    <div className="filter-chip-bar">
      <label className="filter-chip-bar__search">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
        <input placeholder="Search cards, agents, branches…" />
      </label>
      <div className="filter-chip-bar__chips">
        {currentAgent && (
          <button
            type="button"
            className="fchip"
            data-active={isActive('agent', currentAgent)}
            onClick={() => toggle('agent', currentAgent)}
          >
            <span className="fchip__swatch" style={{ background: 'var(--aqua)' }} />
            Mine
          </button>
        )}
        <button type="button" className="fchip" data-active={isActive('priority', 'critical')} onClick={() => toggle('priority', 'critical')}>
          <span className="fchip__swatch" style={{ background: 'var(--red)' }} />
          Critical
        </button>
        <button type="button" className="fchip" data-active={isActive('priority', 'high')} onClick={() => toggle('priority', 'high')}>
          <span className="fchip__swatch" style={{ background: 'var(--orange)' }} />
          High
        </button>
        <button type="button" className="fchip" data-active={isActive('type', 'bug')} onClick={() => toggle('type', 'bug')}>
          <span className="fchip__swatch" style={{ background: 'var(--yellow)' }} />
          Bugs
        </button>
      </div>
    </div>
  );
}
