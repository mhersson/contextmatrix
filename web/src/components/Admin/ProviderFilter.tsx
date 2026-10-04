import { providerCounts, useProviderFilter } from './providerFilter';
import type { SelectorCandidate } from '../../types';

export interface ProviderFilterProps {
  candidates: SelectorCandidate[];
}

/** Compact row of per-provider checkboxes that hides a provider's pills on the ladder. */
export function ProviderFilter({ candidates }: ProviderFilterProps) {
  const { hidden, toggle, selectAll, selectNone } = useProviderFilter(candidates);
  const counts = providerCounts(candidates);

  return (
    <div className="tl-provider-row" aria-label="Provider filter">
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
