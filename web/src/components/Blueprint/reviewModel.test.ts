import { describe, expect, it } from 'vitest';
import type { BlueprintArtifactReview, LogMarker } from '../../types';
import { CARD_REVIEW, SENT_BACK_REVIEW, ref } from '../../test-utils/blueprintFixtures';
import { findingGroups, reviewFor, statusCounts } from './reviewModel';

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
