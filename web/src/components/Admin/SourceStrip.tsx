import { formatRelativeTime } from '../../lib/format';
import type { SelectorCandidatesResponse } from '../../types';

export interface SourceStripProps {
  catalog: SelectorCandidatesResponse;
  /** The catalog loaded; otherwise no number from it is shown. */
  ready: boolean;
  loading: boolean;
}

export function SourceStrip({ catalog, ready, loading }: SourceStripProps) {
  return (
    <div className="tl-source" data-testid="tl-source">
      {ready ? (
        <>
          <span>
            Scores <b>Artificial Analysis Intelligence Index{catalog.intelligence_index_version ? ` v${catalog.intelligence_index_version}` : ''}</b>
          </span>
          <span>
            <b>{catalog.aa_scored_models}</b> AA models scored
          </span>
          <span>
            <b>{catalog.served_scored_models}</b> scored and served by your gateway with tool use
          </span>
          <span>
            <b>{catalog.candidates.length}</b> candidates · floor {catalog.quality_floor}
          </span>
          {catalog.best_aa_score > 0 && (
            <span>
              Best AA score <b>{catalog.best_aa_score}</b> = prior 1.00
            </span>
          )}
          {catalog.reasoning_effort && <span>OpenAI effort {catalog.reasoning_effort}</span>}
          <span>refreshed {formatRelativeTime(catalog.catalog_refreshed_at)}</span>
        </>
      ) : (
        <span>{loading ? 'loading the catalog…' : 'catalog unavailable'}</span>
      )}
      <span>
        Data:{' '}
        <a href="https://artificialanalysis.ai/" target="_blank" rel="noopener noreferrer">
          Artificial Analysis
        </a>
      </span>
    </div>
  );
}
