import { providerCounts } from './providerFilter';
import type { ProviderFilterState } from './providerFilter';
import type { SelectorCandidate } from '../../types';

export interface ProviderFilterProps {
  candidates: SelectorCandidate[];
  /** Filter state owned by the parent, which also hides pills with it. */
  filter: Pick<ProviderFilterState, 'hidden' | 'toggle' | 'selectAll' | 'selectNone'>;
}

/** Compact row of per-provider checkboxes that hides a provider's pills on the ladder. */
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
      {counts.map(({ provider, count }) => (
        <label key={provider} className="tl-provider">
          <input
            type="checkbox"
            className="tl-provider-box"
            checked={!hidden.has(provider)}
            onChange={() => toggle(provider)}
          />
          {provider} {count}
        </label>
      ))}
    </div>
  );
}
