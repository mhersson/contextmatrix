import { useId, useState } from 'react';
import type { BlueprintArtifactReview, BlueprintReviewFinding } from '../../types';
import { severityOf } from './ledger';
import { FINDING_CLAMP_CHARS, SUMMARY_CLAMP_CHARS, cardStatus, findingGroups, type FindingGroup } from './reviewModel';

/** Text past `limit` characters clamps to `lines` lines behind "read more"; shorter text shows whole. */
function Clamped({ text, limit, lines, className }: { text: string; limit: number; lines: 2 | 3; className: string }) {
  const [more, setMore] = useState(false);
  const long = text.length > limit;
  return (
    <>
      <span className={`${className}${long && !more ? ` bp-rv-clamp${lines}` : ''}`}>{text}</span>
      {long && (
        <button type="button" className="bp-rv-more" aria-expanded={more} onClick={() => setMore((m) => !m)}>
          {more ? 'less' : 'read more'}
        </button>
      )}
    </>
  );
}

function FindingRow({ finding: f }: { finding: BlueprintReviewFinding }) {
  const [details, setDetails] = useState(false);
  const id = useId();
  const severity = severityOf(f);
  const status = cardStatus(f);
  return (
    <li className="bp-rv-f">
      <span className="bp-rv-id">{f.id}</span>
      <span className="bp-rv-line">
        {severity && <span className={`bp-rv-sev${severity.level ? ` bp-rv-sev--${severity.level}` : ''}`}>{severity.label}</span>}
        <span className={`bp-rv-st bp-st--${status.tone}`}>{status.text}</span>
      </span>
      <Clamped text={f.text} limit={FINDING_CLAMP_CHARS} lines={2} className="bp-rv-ftext" />
      {(f.reviewer_note || f.addressed_note) && (
        <button
          type="button"
          className="bp-rv-more"
          aria-expanded={details}
          aria-controls={details ? id : undefined}
          onClick={() => setDetails((d) => !d)}
        >
          {details ? 'hide details' : 'details'}
        </button>
      )}
      {details && (
        <div className="bp-rv-detail" id={id}>
          {f.reviewer_note && (
            <p>
              <span className="bp-rv-lbl">Reviewer</span>
              <span>{f.reviewer_note}</span>
            </p>
          )}
          {f.addressed_note && (
            <p>
              <span className="bp-rv-lbl">Builder said</span>
              <span>{f.addressed_note}</span>
            </p>
          )}
        </div>
      )}
    </li>
  );
}

function Group({ group }: { group: FindingGroup }) {
  const [unfolded, setUnfolded] = useState(false);
  const id = useId();
  const folds = group.key === 'fixed';
  const label = `${group.label} (${group.findings.length})`;
  return (
    <>
      <p className="bp-rv-sec" id={id}>
        {folds ? (
          <button type="button" aria-expanded={unfolded} aria-controls={unfolded ? `${id}-list` : undefined} onClick={() => setUnfolded((u) => !u)}>
            <span aria-hidden="true">{unfolded ? '▾' : '▸'}</span> {label}
          </button>
        ) : (
          label
        )}
      </p>
      {(!folds || unfolded) && (
        <ul className="bp-rv-list" id={`${id}-list`} aria-labelledby={id}>
          {group.findings.map((f, i) => (
            <FindingRow key={f.id || i} finding={f} />
          ))}
        </ul>
      )}
    </>
  );
}

/** An opened review card: the reviewer's summary, then the findings by status. */
export function ReviewBody({ id, review }: { id: string; review: BlueprintArtifactReview }) {
  return (
    <div className="bp-rv-body" id={id}>
      {review.summary && (
        <>
          <p className="bp-rv-sec">Summary</p>
          <Clamped text={review.summary} limit={SUMMARY_CLAMP_CHARS} lines={3} className="bp-rv-summary" />
        </>
      )}
      {findingGroups(review).map((g) => (
        <Group key={g.key} group={g} />
      ))}
    </div>
  );
}
