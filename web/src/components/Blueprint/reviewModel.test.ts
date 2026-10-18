import { describe, expect, it } from 'vitest';
import type { BlueprintArtifactReview, LogMarker } from '../../types';
import { CARD_REVIEW, SENT_BACK_REVIEW, ref } from '../../test-utils/blueprintFixtures';
import { cardStatus, countsLine, findingGroups, reviewFor, statusCounts } from './reviewModel';

const marker = (extra: Partial<LogMarker> = {}): LogMarker => ({ kind: 'review', stage: 'acceptance', version: 2, ...extra });

describe('reviewFor', () => {
  const artifacts = [
    ref('acceptance', 2, 'approved', { slice: 'SL1', review: SENT_BACK_REVIEW }),
    ref('acceptance', 2, 'approved', { slice: 'SL2', review: CARD_REVIEW }),
    ref('acceptance', 3, 'reviewing', { slice: 'SL2' }),
  ];

  it("reads the review of the marker's own slice and version", () => {
    expect(reviewFor(artifacts, marker({ slice: 'SL2' }))).toBe(CARD_REVIEW);
    expect(reviewFor(artifacts, marker({ slice: 'SL1' }))).toBe(SENT_BACK_REVIEW);
  });

  it('finds none for a version still in review, another stage, the whole blueprint or a marker without a version', () => {
    expect(reviewFor(artifacts, marker({ slice: 'SL2', version: 3 }))).toBeNull();
    expect(reviewFor(artifacts, marker({ slice: 'SL2', stage: 'solution_design' }))).toBeNull();
    expect(reviewFor(artifacts, marker())).toBeNull();
    expect(reviewFor(artifacts, marker({ slice: 'SL2', version: undefined }))).toBeNull();
  });
});

describe('findingGroups', () => {
  it('groups still open, new and fixed in that order, critical first, and drops empty groups', () => {
    expect(findingGroups(CARD_REVIEW).map((g) => [g.key, g.findings.map((f) => f.id)])).toEqual([
      ['still_open', ['R3']],
      ['new', ['R5', 'R4']],
      ['fixed', ['R2', 'R1']],
    ]);
    expect(findingGroups(SENT_BACK_REVIEW).map((g) => g.key)).toEqual(['new']);
  });

  it('keeps a finding whose status or severity it does not know, with the new ones and last', () => {
    const r: BlueprintArtifactReview = {
      ...SENT_BACK_REVIEW,
      findings: [
        { id: 'R1', severity: 'cosmetic', status: 'disputed', text: 'a' },
        { id: 'R2', severity: 'minor', status: 'open', text: 'b' },
      ],
    };
    expect(findingGroups(r).map((g) => [g.key, g.findings.map((f) => f.id)])).toEqual([['new', ['R2', 'R1']]]);
    expect(statusCounts(r)).toEqual({ still_open: 0, new: 2, fixed: 0 });
  });

  it('leaves the review it groups untouched', () => {
    const before = CARD_REVIEW.findings.map((f) => f.id);
    findingGroups(CARD_REVIEW);
    expect(CARD_REVIEW.findings.map((f) => f.id)).toEqual(before);
  });
});

describe('statusCounts', () => {
  it('counts each group', () => {
    expect(statusCounts(CARD_REVIEW)).toEqual({ still_open: 1, new: 2, fixed: 2 });
  });
});

describe('cardStatus', () => {
  it('tones fixed ok, still open old, and every other status quiet', () => {
    expect(cardStatus({ id: 'a', status: 'fixed', text: 't' }).tone).toBe('ok');
    expect(cardStatus({ id: 'a', status: 'still_open', text: 't' }).tone).toBe('old');
    expect(cardStatus({ id: 'a', status: 'open', text: 't' }).tone).toBe('muted');
    expect(cardStatus({ id: 'a', status: 'something_new', text: 't' }).tone).toBe('muted');
  });

  it('names the verifying rev on a fixed finding only when there is one', () => {
    const set = cardStatus({ id: 'a', status: 'fixed', verified_in: 7, text: 't' }).text;
    const unset = cardStatus({ id: 'a', status: 'fixed', text: 't' }).text;
    expect(set).toContain('7');
    expect(unset).not.toContain('7');
    expect(unset).not.toBe(set);
  });
});

describe('countsLine', () => {
  const base: BlueprintArtifactReview = { ...CARD_REVIEW, round: undefined, max_rounds: undefined };

  it('drops zero counts and says something when there are no findings', () => {
    const noneFixed = { ...base, findings: base.findings.filter((f) => f.status !== 'fixed') };
    expect(countsLine(noneFixed)).not.toMatch(/\b0\b/);
    expect(countsLine({ ...base, findings: [] })).not.toBe('');
    expect(countsLine({ ...base, findings: [] })).not.toMatch(/\b0\b/);
  });

  it('names the round limit only when both the round and the limit are set', () => {
    const r = { ...base, round: 2, max_rounds: 9 };
    expect(countsLine(r)).toContain('9');
    expect(countsLine({ ...r, max_rounds: 0 })).not.toContain('9');
    expect(countsLine({ ...r, round: 0 })).not.toContain('9');
  });
});
