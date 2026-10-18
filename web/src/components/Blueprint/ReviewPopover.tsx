import { FindingList } from './FindingList';
import { verdictLine } from './ledger';
import type { ShownReview } from './sheetModel';
import { personaName } from './stages';

export function ReviewPopover({ review: { review, version }, maxHeight }: { review: ShownReview; maxHeight?: number }) {
  return (
    <div className="bp-review" style={{ maxHeight }} role="dialog" aria-label={`Review of rev ${version}`}>
      <b>
        {personaName(review.reviewer)} scored rev {version} {review.score} of 5
      </b>
      <small>{verdictLine(review)}</small>
      {review.findings.length === 0 ? <p>No findings.</p> : <FindingList findings={review.findings} />}
    </div>
  );
}
