import type { CardFilter } from '../../types';

interface FilterChipBarProps {
  filter: CardFilter;
  currentAgent: string | null;
  onFilterChange: (filter: CardFilter) => void;
  searchQuery?: string;
  onSearchChange?: (query: string) => void;
}

type StringFilterKey = Exclude<keyof CardFilter, 'vetted' | 'autonomous'>;

/**
 * Replaces the dropdown-style FilterBar with a chip-driven filter row.
 * The chips are visually prominent so the active filter is obvious at
 * a glance. Owns the search input + view toggle.
 */
export function FilterChipBar({
  filter,
  currentAgent,
  onFilterChange,
  searchQuery = '',
  onSearchChange,
}: FilterChipBarProps) {
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
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
        <input
          type="search"
          aria-label="Search cards"
          placeholder="Search cards (ID, title, label)…"
          value={searchQuery}
          onChange={(e) => onSearchChange?.(e.target.value)}
        />
      </label>
      <div className="filter-chip-bar__chips">
        {currentAgent && (
          <button
            type="button"
            className="fchip"
            data-active={isActive('agent', currentAgent)}
            aria-pressed={isActive('agent', currentAgent)}
            onClick={() => toggle('agent', currentAgent)}
          >
            <span className="fchip__swatch" style={{ background: 'var(--aqua)' }} />
            Mine
          </button>
        )}
        <button
          type="button"
          className="fchip"
          data-active={isActive('priority', 'critical')}
          aria-pressed={isActive('priority', 'critical')}
          onClick={() => toggle('priority', 'critical')}
        >
          <span className="fchip__swatch" style={{ background: 'var(--red)' }} />
          Critical
        </button>
        <button
          type="button"
          className="fchip"
          data-active={isActive('priority', 'high')}
          aria-pressed={isActive('priority', 'high')}
          onClick={() => toggle('priority', 'high')}
        >
          <span className="fchip__swatch" style={{ background: 'var(--orange)' }} />
          High
        </button>
        <button
          type="button"
          className="fchip"
          data-active={isActive('type', 'bug')}
          aria-pressed={isActive('type', 'bug')}
          onClick={() => toggle('type', 'bug')}
        >
          <span className="fchip__swatch" style={{ background: 'var(--yellow)' }} />
          Bugs
        </button>
        <button
          type="button"
          className="fchip"
          data-active={filter.autonomous === true}
          aria-pressed={filter.autonomous === true}
          onClick={() => onFilterChange(
            filter.autonomous ? { ...filter, autonomous: undefined } : { ...filter, autonomous: true }
          )}
        >
          <span className="fchip__swatch" style={{ background: 'var(--purple)' }} />
          Autonomous
        </button>
        <button
          type="button"
          className="fchip"
          data-active={filter.worker_status === 'running'}
          aria-pressed={filter.worker_status === 'running'}
          onClick={() => toggle('worker_status', 'running')}
        >
          <span className="fchip__swatch" style={{ background: 'var(--aqua)' }} />
          worker:running
        </button>
      </div>
    </div>
  );
}
