import { useId, useRef, useState } from 'react';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import { ReviewPopover } from './ReviewPopover';
import type { ShownReview, ShownStatus, VersionOption } from './sheetModel';
import { personaName, stageText } from './stages';

interface TitleBlockProps {
  stage: string;
  /** The stage's persona: who drew the sheet. */
  persona: string;
  options: VersionOption[];
  version: number;
  onPickVersion: (version: number) => void;
  review: ShownReview | null;
  /** The reviewer persona at work on this version, if any. */
  reviewing: string | null;
  status: ShownStatus;
  compare: { available: boolean; on: boolean; onToggle: () => void };
}

export function TitleBlock({ stage, persona, options, version, onPickVersion, review, reviewing, status, compare }: TitleBlockProps) {
  const revLabel = useId();
  const [reviewOpen, setReviewOpen] = useState(false);
  const checkedRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  useMenuDismiss(checkedRef, reviewOpen, () => setReviewOpen(false), triggerRef);
  return (
    <div className="bp-tb" role="group" aria-label="Title block">
      <div>
        <small>Sheet</small>
        <span className="bp-tb-value bp-tb-name">{stageText(stage).tab}</span>
      </div>
      <div>
        <small id={revLabel}>Rev</small>
        {options.length > 1 ? (
          <select aria-labelledby={revLabel} value={version} onChange={(e) => onPickVersion(Number(e.target.value))}>
            {options.map((o) => (
              <option key={o.version} value={o.version}>
                {o.label}
              </option>
            ))}
          </select>
        ) : (
          <span className="bp-tb-value">{version}</span>
        )}
        {compare.available && (
          <label className="bp-tb-compare">
            <input type="checkbox" checked={compare.on} onChange={compare.onToggle} />
            Compare
          </label>
        )}
      </div>
      <div>
        <small>Drawn by</small>
        <span className="bp-tb-value">{personaName(persona)}</span>
      </div>
      {/* Marks Escape handled so an enclosing overlay does not also close; useMenuDismiss closes the popover. */}
      <div
        ref={checkedRef}
        onKeyDown={(e) => {
          if (reviewOpen && e.key === 'Escape') e.preventDefault();
        }}
      >
        <small>Checked by</small>
        {reviewing ? (
          <span className="bp-tb-value">{personaName(reviewing)}, reviewing</span>
        ) : review ? (
          <>
            <button
              ref={triggerRef}
              type="button"
              className="bp-tb-link"
              aria-expanded={reviewOpen}
              onClick={() => setReviewOpen((o) => !o)}
            >
              {personaName(review.review.reviewer)}, {review.review.score} of 5
            </button>
            {reviewOpen && <ReviewPopover review={review} />}
          </>
        ) : (
          <span className="bp-tb-value">Contract checks</span>
        )}
      </div>
      <div>
        <small>Status</small>
        <span className={`bp-tb-value bp-st--${status.tone}`}>{status.text}</span>
      </div>
    </div>
  );
}
