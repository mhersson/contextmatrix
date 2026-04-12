import { useMemo, forwardRef } from 'react';
import type { Card, CardFilter, ProjectConfig } from '../../types';

interface FilterBarProps {
  config: ProjectConfig;
  cards: Card[];
  filter: CardFilter;
  onFilterChange: (filter: CardFilter) => void;
}

export const FilterBar = forwardRef<HTMLDivElement, FilterBarProps>(
  function FilterBar({ config, cards, filter, onFilterChange }, ref) {
    const labels = useMemo(
      () => [...new Set(cards.flatMap((c) => c.labels ?? []))].sort(),
      [cards]
    );

    const agents = useMemo(
      () =>
        [...new Set(cards.filter((c) => c.assigned_agent).map((c) => c.assigned_agent!))].sort(),
      [cards]
    );

    const hasFilter = Object.values(filter).some(Boolean);

    function update(field: keyof CardFilter, value: string) {
      onFilterChange({ ...filter, [field]: value || undefined });
    }

    const selectClass =
      'w-full sm:w-auto px-2 py-1 text-sm rounded border bg-[var(--bg1)] border-[var(--bg3)] text-[var(--fg)]';

    return (
      <div
        ref={ref}
        className="grid grid-cols-2 sm:flex sm:flex-wrap items-center gap-2 px-4 py-2 border-b border-[var(--bg3)]"
      >
        <select
          value={filter.type ?? ''}
          onChange={(e) => update('type', e.target.value)}
          className={selectClass}
          aria-label="Filter by type"
        >
          <option value="">All types</option>
          {config.types.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>

        <select
          value={filter.priority ?? ''}
          onChange={(e) => update('priority', e.target.value)}
          className={selectClass}
          aria-label="Filter by priority"
        >
          <option value="">All priorities</option>
          {config.priorities.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>

        <select
          value={filter.label ?? ''}
          onChange={(e) => update('label', e.target.value)}
          className={selectClass}
          aria-label="Filter by label"
        >
          <option value="">All labels</option>
          {labels.map((l) => (
            <option key={l} value={l}>
              {l}
            </option>
          ))}
        </select>

        <select
          value={filter.agent ?? ''}
          onChange={(e) => update('agent', e.target.value)}
          className={selectClass}
          aria-label="Filter by agent"
        >
          <option value="">All agents</option>
          {agents.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>

        {hasFilter && (
          <button
            onClick={() => onFilterChange({})}
            className="col-span-2 sm:col-span-1 px-2 py-1 text-sm text-[var(--grey1)] hover:text-[var(--red)] transition-colors"
            aria-label="Clear all filters"
          >
            Clear
          </button>
        )}
      </div>
    );
  }
);
