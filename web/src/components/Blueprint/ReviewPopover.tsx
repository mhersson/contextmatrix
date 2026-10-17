import type { BlueprintReviewFinding } from '../../types';
import type { ShownReview } from './sheetModel';
import { personaName } from './stages';

export function ReviewPopover({ review: { review, version }, maxHeight }: { review: ShownReview; maxHeight?: number }) {
  return (
    <div className="bp-review" style={{ maxHeight }} role="dialog" aria-label={`Review of rev ${version}`}>
      <b>
        {personaName(review.reviewer)} scored rev {version} {review.score} of 5
      </b>
      <small>Passes at {review.threshold} of 5</small>
      {review.findings.length === 0 ? (
        <p>No findings.</p>
      ) : (
        <ol>
          {review.findings.map((f, i) => (
            <li key={i}>
              {f.text} <FindingMark finding={f} />
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

// Nothing re-reviews a revision, so a finding is never "fixed": only addressed when the revising run said so.
function FindingMark({ finding: f }: { finding: BlueprintReviewFinding }) {
  if (f.addressed_in) {
    return (
      <span className="bp-fix">
        addressed in rev {f.addressed_in}
        {f.addressed_by ? ` by ${personaName(f.addressed_by)}` : ''}
        {f.addressed_note ? `: ${f.addressed_note}` : ''}
      </span>
    );
  }
  if (f.sent_to) return <span className="bp-sent">sent to rev {f.sent_to}</span>;
  return null;
}
