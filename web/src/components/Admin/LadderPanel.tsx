import { useMemo } from 'react';
import type { CSSProperties } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorTier, SelectorTierPreview } from '../../types';
import { ProviderFilter } from './ProviderFilter';
import { SelectionChart } from './SelectionChart';
import { useProviderFilter } from './useProviderFilter';

export interface LadderPanelProps {
  candidates: SelectorCandidate[];
  ladders: SelectorLadders;
  /** Two ladders on screen. */
  separate: boolean;
  onSeparateChange: (next: boolean) => void;
  onChange: (next: SelectorLadders) => void;
  /** The price headroom being edited; NaN while the field is emptied. */
  headroom: number;
  onHeadroomChange: (headroom: number) => void;
  floor: number;
  bestAAScore: number;
  indexVersion: string;
  selectedTier: SelectorTier;
  tierPreview: SelectorTierPreview | null;
  appliedHeadroom: number | null;
  blacklist: ReadonlySet<string>;
  /** Right-click on a dot: the slug and the pointer's viewport position. Unset leaves the browser menu alone. */
  onModelMenu?: (slug: string, x: number, y: number) => void;
}

export function LadderPanel({
  candidates,
  ladders,
  separate,
  onSeparateChange,
  onChange,
  headroom,
  onHeadroomChange,
  floor,
  bestAAScore,
  indexVersion,
  selectedTier,
  tierPreview,
  appliedHeadroom,
  blacklist,
  onModelMenu,
}: LadderPanelProps) {
  // View-only filter: the dots it hides never move a scale or touch a ladder.
  const { visible, isActive, toggle, selectAll, selectNone, hidden } = useProviderFilter(candidates);
  const shown = useMemo(() => candidates.filter(visible).length, [candidates, visible]);

  return (
    <section className="apd-panel" style={{ '--apd-acc': 'var(--aqua)' } as CSSProperties}>
      <div className="apd-panel-head">
        <h2 className="apd-panel-title">Tier ladder</h2>
        <div className="apd-panel-meta">
          {isActive && (
            <span>
              {shown} of {candidates.length} candidates shown
            </span>
          )}
          <label className="tl-headroom">
            price headroom
            <input
              type="number"
              min={1}
              step={0.1}
              value={Number.isNaN(headroom) ? '' : headroom}
              aria-label="Price headroom"
              aria-invalid={Number.isFinite(headroom) && headroom >= 1 ? undefined : true}
              onChange={(e) => onHeadroomChange(e.target.value === '' ? NaN : Number(e.target.value))}
            />
            ×
          </label>
          <button
            type="button"
            role="switch"
            aria-checked={separate}
            aria-label="Separate reviewer ladder"
            className={`tl-switch${separate ? ' on' : ''}`}
            onClick={() => onSeparateChange(!separate)}
          >
            <span className="tl-switch-knob" aria-hidden="true" />
            separate reviewer ladder
          </button>
        </div>
      </div>
      <div className="tl-panel-body">
        <ProviderFilter candidates={candidates} filter={{ hidden, toggle, selectAll, selectNone }} />
        <SelectionChart
          candidates={candidates}
          visible={visible}
          ladders={ladders}
          separate={separate}
          floor={floor}
          bestAAScore={bestAAScore}
          indexVersion={indexVersion}
          selectedTier={selectedTier}
          tierPreview={tierPreview}
          appliedHeadroom={appliedHeadroom}
          blacklist={blacklist}
          onChange={onChange}
          onModelMenu={onModelMenu}
        />
        <div className="tl-legend">
          <span data-testid="tl-legend-coder">
            <span className="tl-sw" style={{ '--sw-c': 'var(--tl-coder)' } as CSSProperties} aria-hidden="true" />
            coder
          </span>
          <span data-testid="tl-legend-reviewer">
            <span className="tl-sw" style={{ '--sw-c': 'var(--tl-reviewer)' } as CSSProperties} aria-hidden="true" />
            reviewer
          </span>
          {!separate && (
            <span data-testid="tl-legend-shared">
              <span className="tl-sw" style={{ '--sw-c': 'var(--tl-shared)' } as CSSProperties} aria-hidden="true" />
              shared tier lines
            </span>
          )}
          <span className="tl-legend-note">
            filled dot = the pick at the selected tier · dashed ring = a panel seat · struck = blacklisted
            {onModelMenu && ' · right-click a dot to blacklist or delist'}
          </span>
        </div>
      </div>
    </section>
  );
}
