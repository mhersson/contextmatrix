import { providerCounts } from './useProviderFilter';
import type { ProviderFilterState } from './useProviderFilter';
import type { SelectorCandidate } from '../../types';

export interface ProviderFilterProps {
  candidates: SelectorCandidate[];
  /** Filter state owned by the parent, which also hides dots with it. */
  filter: Pick<ProviderFilterState, 'hidden' | 'toggle' | 'selectAll' | 'selectNone'>;
}

/** Row of per-provider chips; an unpressed chip hides that provider's dots. */
export function ProviderFilter({ candidates, filter }: ProviderFilterProps) {
  const { hidden, toggle, selectAll, selectNone } = filter;
  const counts = providerCounts(candidates);

  return (
    <div className="tl-provider-row" role="group" aria-label="Provider filter">
      <button type="button" className="tl-provider-btn" onClick={selectAll}>
        all
      </button>
      <button type="button" className="tl-provider-btn" onClick={selectNone}>
        none
      </button>
      {counts.map(({ provider, count }) => {
        const shown = !hidden.has(provider);
        return (
          <button
            key={provider}
            type="button"
            className={`tl-chip${shown ? '' : ' off'}`}
            aria-pressed={shown}
            onClick={() => toggle(provider)}
          >
            {provider} <small>{count}</small>
          </button>
        );
      })}
    </div>
  );
}
