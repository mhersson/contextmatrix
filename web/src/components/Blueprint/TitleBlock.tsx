import { useId, useLayoutEffect, useRef, useState } from 'react';
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
  // The score button unmounts while a review runs; close here so the popover cannot reappear when it ends.
  if (reviewing && reviewOpen) setReviewOpen(false);
  const checkedRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const blockRef = useRef<HTMLDivElement>(null);
  const [maxHeight, setMaxHeight] = useState<number | undefined>(undefined);
  useMenuDismiss(checkedRef, reviewOpen, () => setReviewOpen(false), triggerRef);
  // The popover opens upwards: cap it to the room between the sheet top and the title block.
  useLayoutEffect(() => {
    if (!reviewOpen) return;
    const measure = () => {
      const block = blockRef.current;
      if (!block) return;
      const sheetTop = block.closest('.bp-sheet')?.getBoundingClientRect().top ?? 0;
      setMaxHeight(Math.max(80, block.getBoundingClientRect().top - sheetTop - 12));
    };
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [reviewOpen]);
  return (
    <div ref={blockRef} className="bp-tb" role="group" aria-label="Title block">
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
            {reviewOpen && <ReviewPopover review={review} maxHeight={maxHeight} />}
          </>
        ) : (
          <span className="bp-tb-value">Contract checks</span>
        )}
      </div>
      <div>
        <small>Status</small>
        <span className={`bp-tb-value bp-st--${status.tone}`} title={status.text}>{status.text}</span>
      </div>
    </div>
  );
}
