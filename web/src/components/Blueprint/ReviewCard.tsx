import { useId, useState, type ReactNode } from 'react';
import type { LogMarker } from '../../types';
import { ChevronIcon, ReviewIcon } from './icons';
import { ReviewBody } from './ReviewBody';
import { cardVerdict, countsLine, explainText, reviewFor } from './reviewModel';
import { useBlueprintArtifacts, useBlueprintSheet } from './sheetContext';
import { personaName, sliceOf, sliceSuffix, stageText } from './stages';

/** A reviewer's verdict on one version, drawn from the view's review; the marker's line until the view holds it. */
export function ReviewCard({ marker, fallback }: { marker: LogMarker; fallback: ReactNode }) {
  const review = reviewFor(useBlueprintArtifacts(), marker);
  const slices = useBlueprintSheet()?.slices ?? [];
  const [open, setOpen] = useState(false);
  const id = useId();
  if (!review) return fallback;

  const verdict = cardVerdict(review);
  const tone = verdict === 'passed' ? ' bp-rv--pass' : verdict === 'failed' ? ' bp-rv--fail' : '';
  const title = `${personaName(marker.persona || review.reviewer)} reviewed ${stageText(marker.stage).name}${sliceSuffix(slices, sliceOf(marker))} rev ${marker.version}`;
  const openable = !!review.summary || review.findings.length > 0;
  const explain = explainText(review);
  const text = (
    <>
      <ReviewIcon passed={verdict === 'passed'} />
      <span className="bp-rv-text">
        <b>{title}</b>
        {verdict && <> <span className="bp-rv-verdict">{verdict}</span></>}
        <span className="bp-rv-help" title={explain} aria-hidden="true">
          ?
        </span>
        <span className="bp-rv-note">{countsLine(review)}</span>
      </span>
    </>
  );
  return (
    <div className="flex justify-start">
      {/* The transcript is a polite live region; opening the card must not read every finding aloud. */}
      <div
        className={`bp-rv${tone}${open ? ' open' : ''}`}
        role="group"
        aria-label={title}
        aria-live="off"
        aria-describedby={openable ? undefined : `${id}-explain`}
      >
        {openable ? (
          <button
            type="button"
            className="bp-rv-head"
            aria-expanded={open}
            aria-controls={open ? `${id}-body` : undefined}
            aria-describedby={`${id}-explain`}
            onClick={() => setOpen((o) => !o)}
          >
            {text}
            <ChevronIcon className="bp-rv-chev" />
          </button>
        ) : (
          <div className="bp-rv-head">{text}</div>
        )}
        <span id={`${id}-explain`} hidden>
          {explain}
        </span>
        {open && openable && <ReviewBody id={`${id}-body`} review={review} />}
      </div>
    </div>
  );
}
