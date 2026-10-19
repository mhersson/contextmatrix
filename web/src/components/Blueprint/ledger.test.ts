import { describe, it, expect } from 'vitest';
import type { BlueprintReviewFinding } from '../../types';
import { DESIGN_REVIEW, LEDGER_REVIEW_FAILED, LEDGER_REVIEW_V1, LEDGER_REVIEW_V2 } from '../../test-utils/blueprintFixtures';
import {
  blockingFindings,
  claimText,
  findingStatus,
  hasLedger,
  sentText,
  severityOf,
  unresolvedCount,
  verdictLine,
  verdictOf,
} from './ledger';

const finding = (over: Partial<BlueprintReviewFinding>): BlueprintReviewFinding => ({ text: 'x', ...over });

describe('finding status', () => {
  it('reads the reviewer verdict with the version it was checked against', () => {
    expect(findingStatus(finding({ status: 'open' }))).toEqual({ text: 'open', tone: 'muted' });
    expect(findingStatus(finding({ status: 'fixed', verified_in: 2 }))).toEqual({ text: 'fixed in rev 2', tone: 'ok' });
    expect(findingStatus(finding({ status: 'still_open', verified_in: 3 }))).toEqual({ text: 'still open in rev 3', tone: 'bad' });
  });

  it('drops the version when the verdict names none', () => {
    expect(findingStatus(finding({ status: 'fixed' }))?.text).toBe('fixed');
    expect(findingStatus(finding({ status: 'still_open', verified_in: 0 }))?.text).toBe('still open');
  });

  it('shows an unknown status neutrally and none for an older architect', () => {
    expect(findingStatus(finding({ status: 'wont_fix' }))).toEqual({ text: 'wont fix', tone: 'muted' });
    expect(findingStatus(finding({}))).toBeNull();
    expect(findingStatus(finding({ status: '' }))).toBeNull();
  });
});

describe('severity', () => {
  it('maps the three levels and keeps an unknown one neutral', () => {
    expect(severityOf(finding({ severity: 'critical' }))).toEqual({ label: 'critical', level: 'critical' });
    expect(severityOf(finding({ severity: 'minor' }))).toEqual({ label: 'minor', level: 'minor' });
    expect(severityOf(finding({ severity: 'blocker' }))).toEqual({ label: 'blocker', level: null });
    expect(severityOf(finding({}))).toBeNull();
  });
});

describe('claim', () => {
  it('quotes the builder as a claim', () => {
    expect(claimText(finding({ addressed_in: 2, addressed_note: 'Uses SetIndent.' }))).toBe('rev 2 says: Uses SetIndent.');
    expect(claimText(finding({ addressed_in: 2 }))).toBe('rev 2 says it is addressed');
    expect(claimText(finding({ sent_to: 2 }))).toBeNull();
  });

  it('says where an unclaimed finding went', () => {
    expect(sentText(finding({ sent_to: 2 }))).toBe('sent to rev 2');
    expect(sentText(finding({ sent_to: 2, addressed_in: 2 }))).toBeNull();
    expect(sentText(finding({}))).toBeNull();
  });
});

describe('blocking findings', () => {
  it('holds every critical finding the reviewer has not marked fixed', () => {
    expect(blockingFindings(LEDGER_REVIEW_FAILED).map((f) => f.id)).toEqual(['R5']);
    expect(blockingFindings(LEDGER_REVIEW_V1).map((f) => f.id)).toEqual(['R2']);
  });

  it('lets a fixed critical finding through', () => {
    expect(blockingFindings(LEDGER_REVIEW_V2)).toEqual([]);
  });

  it('treats a critical finding without a status as not fixed', () => {
    expect(blockingFindings({ ...LEDGER_REVIEW_V2, findings: [finding({ severity: 'critical' })] })).toHaveLength(1);
  });

  it('never blocks on an older architect review', () => {
    expect(blockingFindings(DESIGN_REVIEW)).toEqual([]);
  });
});

describe('verdict', () => {
  it('takes the reviewer verdict', () => {
    expect(verdictOf(LEDGER_REVIEW_V2)).toBe('passed');
    expect(verdictOf(LEDGER_REVIEW_FAILED)).toBe('failed');
  });

  it('fails a review an open critical finding holds even when it says passed', () => {
    expect(verdictOf({ ...LEDGER_REVIEW_FAILED, passed: true })).toBe('failed');
  });

  it('has no verdict for an older architect', () => {
    expect(verdictOf(DESIGN_REVIEW)).toBeNull();
    expect(verdictLine(DESIGN_REVIEW)).toBe('Passes at 4 of 5');
  });

  it('states the round, the pass mark and the verdict', () => {
    expect(verdictLine(LEDGER_REVIEW_FAILED)).toBe('Round 3 · passes at 4 of 5 · failed');
  });

  it('ends with sent back exactly when the review went back for a revision', () => {
    const last = (r: Parameters<typeof verdictLine>[0]) => verdictLine(r).split(' · ').at(-1);
    expect(last({ ...LEDGER_REVIEW_V2, sent_back: true })).toBe('sent back');
    expect(last({ ...LEDGER_REVIEW_FAILED, sent_back: true })).toBe('sent back');
    expect(last({ ...LEDGER_REVIEW_V2, sent_back: false })).toBe('passed');
    expect(last(LEDGER_REVIEW_FAILED)).toBe('failed');
  });
});

describe('ledger shape', () => {
  it('tells a ledger from an older architect list', () => {
    expect(hasLedger(LEDGER_REVIEW_V1.findings)).toBe(true);
    expect(hasLedger(DESIGN_REVIEW.findings)).toBe(false);
  });

  it('counts what a revision still has to answer', () => {
    expect(unresolvedCount(LEDGER_REVIEW_V2)).toBe(2);
    expect(unresolvedCount(DESIGN_REVIEW)).toBe(3);
  });
});
