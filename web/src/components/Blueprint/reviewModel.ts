import type { BlueprintArtifactRef, BlueprintArtifactReview, BlueprintReviewFinding, LogMarker } from '../../types';
import { findingStatus, verdictOf } from './ledger';
import { sliceOf } from './stages';

/** Text past these lengths clamps behind "read more"; shorter text never clamps, so none hides without a way to read it. */
export const FINDING_CLAMP_CHARS = 160;
export const SUMMARY_CLAMP_CHARS = 240;

/** The review the view holds for the marker's stage, slice and version; null shows the marker's line instead. */
export function reviewFor(artifacts: readonly BlueprintArtifactRef[], m: LogMarker): BlueprintArtifactReview | null {
  if (!m.version) return null;
  const ref = artifacts.find((a) => a.stage === m.stage && sliceOf(a) === sliceOf(m) && a.version === m.version);
  return ref?.review ?? null;
}

export type GroupKey = 'still_open' | 'new' | 'fixed';

/** Anything but fixed or still open was raised this round, so a status this UI does not know is never dropped. */
export function groupOf(f: BlueprintReviewFinding): GroupKey {
  return f.status === 'fixed' || f.status === 'still_open' ? f.status : 'new';
}

export function statusCounts(r: BlueprintArtifactReview): Record<GroupKey, number> {
  const counts: Record<GroupKey, number> = { still_open: 0, new: 0, fixed: 0 };
  for (const f of r.findings) counts[groupOf(f)]++;
  return counts;
}

export interface FindingGroup {
  key: GroupKey;
  label: string;
  findings: BlueprintReviewFinding[];
}

const GROUPS: readonly { key: GroupKey; label: string }[] = [
  { key: 'still_open', label: 'Still open' },
  { key: 'new', label: 'New this round' },
  { key: 'fixed', label: 'Fixed' },
];

const RANK: Record<string, number> = { critical: 0, major: 1, minor: 2 };
const rank = (f: BlueprintReviewFinding) => (f.severity && f.severity in RANK ? RANK[f.severity] : 3);

/** Non-empty groups in card order, critical first; equal severities keep the reviewer's order. */
export function findingGroups(r: BlueprintArtifactReview): FindingGroup[] {
  return GROUPS.map((g) => ({ ...g, findings: r.findings.filter((f) => groupOf(f) === g.key).sort((a, b) => rank(a) - rank(b)) })).filter(
    (g) => g.findings.length > 0,
  );
}

export function countsLine(r: BlueprintArtifactReview): string {
  const c = statusCounts(r);
  const parts: string[] = [];
  if (c.fixed) parts.push(`${c.fixed} fixed`);
  if (c.still_open) parts.push(`${c.still_open} still open`);
  if (c.new) parts.push(`${c.new} new`);
  if (parts.length === 0) parts.push('no findings');
  if (r.round && r.max_rounds) parts.push(`round ${r.round} of ${r.max_rounds}`);
  return parts.join(' · ');
}

export function explainText(r: BlueprintArtifactReview): string {
  const last = r.max_rounds ? `, and round ${r.max_rounds} goes to the gate whatever its verdict` : '';
  return `The reviewer grades each version from 1 to 5. A version passes with ${r.threshold} or more and no open critical finding. A version that passes with no open major finding goes to the gate with any open minor findings; any other goes back for a revision${last}.`;
}

export type CardVerdict = 'passed' | 'sent back' | 'failed';

/** The architect's sent_back flag decides, so a pass that went back for its open major finding never reads passed. */
export function cardVerdict(r: BlueprintArtifactReview): CardVerdict | null {
  if (r.sent_back) return 'sent back';
  const v = verdictOf(r);
  return v === 'passed' ? 'passed' : v === 'failed' ? 'failed' : null;
}

export interface CardStatus {
  text: string;
  /** The .bp-st--<tone> class: ink green, ink orange, quiet. */
  tone: 'ok' | 'old' | 'muted';
}

export function cardStatus(f: BlueprintReviewFinding): CardStatus {
  switch (groupOf(f)) {
    case 'fixed':
      return { text: findingStatus(f)?.text ?? 'fixed', tone: 'ok' };
    case 'still_open':
      return { text: 'still open', tone: 'old' };
    default:
      return { text: 'new', tone: 'muted' };
  }
}
