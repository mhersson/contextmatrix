import type { BlueprintReviewFinding } from '../../types';
import { claimText, findingStatus, hasLedger, isBlocking, sentText, severityOf } from './ledger';

/** A review's findings: the ledger, whose ids are its numbers, or an older architect's numbered list. */
export function FindingList({ findings }: { findings: readonly BlueprintReviewFinding[] }) {
  const items = findings.map((f, i) => <FindingItem key={i} finding={f} />);
  return hasLedger(findings) ? (
    <ul className="bp-ledger" aria-label="Findings">
      {items}
    </ul>
  ) : (
    <ol aria-label="Findings">{items}</ol>
  );
}

function FindingItem({ finding: f }: { finding: BlueprintReviewFinding }) {
  const severity = severityOf(f);
  const status = findingStatus(f);
  const claim = claimText(f);
  const sent = sentText(f);
  return (
    <li className={`bp-finding${isBlocking(f) ? ' bp-finding--blocking' : ''}`}>
      {(f.id || severity || status) && (
        <span className="bp-finding-head">
          {f.id && <b className="bp-finding-id">{f.id}</b>}
          {severity && (
            <span className={`bp-sev${severity.level ? ` bp-sev--${severity.level}` : ''}`}>{severity.label}</span>
          )}
          {status && <span className={`bp-st--${status.tone}`}>{status.text}</span>}
        </span>
      )}
      {f.text}
      {claim ? <span className="bp-claim">{claim}</span> : sent ? <span className="bp-sent">{sent}</span> : null}
    </li>
  );
}
