import type { BlueprintArtifactReview, BlueprintReviewFinding } from '../../types';

// Only Status is the reviewer's verdict; the addressed fields are the builder's claim and never read as fixed.

export type Severity = 'critical' | 'major' | 'minor';

export interface SeverityText {
  label: string;
  /** Null for a severity this UI does not know; it shows neutrally. */
  level: Severity | null;
}

const SEVERITIES: ReadonlySet<string> = new Set(['critical', 'major', 'minor']);

export function severityOf(f: BlueprintReviewFinding): SeverityText | null {
  if (!f.severity) return null;
  return { label: f.severity, level: SEVERITIES.has(f.severity) ? (f.severity as Severity) : null };
}

export interface FindingStatusText {
  text: string;
  tone: 'ok' | 'bad' | 'muted';
}

export function findingStatus(f: BlueprintReviewFinding): FindingStatusText | null {
  if (!f.status) return null;
  const rev = f.verified_in ? ` in rev ${f.verified_in}` : '';
  switch (f.status) {
    case 'open':
      return { text: 'open', tone: 'muted' };
    case 'fixed':
      return { text: `fixed${rev}`, tone: 'ok' };
    case 'still_open':
      return { text: `still open${rev}`, tone: 'bad' };
    default:
      return { text: f.status.replace(/_/g, ' '), tone: 'muted' };
  }
}

export function claimText(f: BlueprintReviewFinding): string | null {
  if (!f.addressed_in) return null;
  return f.addressed_note ? `rev ${f.addressed_in} says: ${f.addressed_note}` : `rev ${f.addressed_in} says it is addressed`;
}

/** Where a finding went when that revision made no claim about it. */
export function sentText(f: BlueprintReviewFinding): string | null {
  return f.sent_to && !f.addressed_in ? `sent to rev ${f.sent_to}` : null;
}

/** A critical finding the reviewer has not marked fixed fails the review whatever the score. */
export function isBlocking(f: BlueprintReviewFinding): boolean {
  return f.severity === 'critical' && f.status !== 'fixed';
}

export function blockingFindings(r: BlueprintArtifactReview): BlueprintReviewFinding[] {
  return r.findings.filter(isBlocking);
}

/** Findings a revision still has to answer; an older architect's carry no status, so all count. */
export function unresolvedCount(r: BlueprintArtifactReview): number {
  return r.findings.filter((f) => f.status !== 'fixed').length;
}

/** Null for an older architect's review, which carries no verdict. */
export function verdictOf(r: BlueprintArtifactReview): 'passed' | 'failed' | null {
  if (r.passed === false || blockingFindings(r).length > 0) return 'failed';
  return r.passed === true ? 'passed' : null;
}

export function verdictLine(r: BlueprintArtifactReview): string {
  const line = [r.round ? `round ${r.round}` : '', `passes at ${r.threshold} of 5`, verdictOf(r) ?? '', r.sent_back ? 'sent back' : '']
    .filter(Boolean)
    .join(' · ');
  return line[0].toUpperCase() + line.slice(1);
}

/** Reviews from protocol v0.28.0 on give every finding an id. */
export function hasLedger(findings: readonly BlueprintReviewFinding[]): boolean {
  return findings.some((f) => !!f.id);
}
