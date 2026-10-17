import { describe, it, expect } from 'vitest';
import { formatHHMM } from '../../utils/chatTimestamp';
import { ref, viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import { reviewFor, shownStatus, stampFor, versionOptions } from './sheetModel';

describe('stampFor', () => {
  const view = viewAtDesignGate();

  it('stamps superseded, folded, approved and rejected versions', () => {
    expect(stampFor(view, view.artifacts[2])).toEqual({ tone: 'superseded', title: 'Superseded', detail: 'by rev 2' });
    expect(stampFor(view, view.artifacts[1])).toEqual({ tone: 'folded', title: 'Folded', detail: 'into design' });
    const at = '2026-10-17T10:17:00Z';
    expect(stampFor(view, ref('solution_design', 2, 'approved', { decided_by: 'you', decided_at: at }))).toEqual({
      tone: 'approved',
      title: 'Approved',
      detail: `by you, ${formatHHMM(at)}`,
    });
    expect(stampFor(view, ref('solution_design', 2, 'rejected', { decided_by: 'responder' }))?.detail).toBe('by the responder');
  });

  it('leaves undecided versions unstamped', () => {
    expect(stampFor(view, view.artifacts[3])).toBeNull();
    expect(stampFor(view, view.artifacts[0])).toBeNull();
  });
});

describe('versionOptions', () => {
  it('labels older versions and a version being written', () => {
    const view = viewAtDesignGate();
    expect(versionOptions(view, 'solution_design', null)).toEqual([
      { version: 1, label: '1, superseded' },
      { version: 2, label: '2' },
    ]);
    expect(versionOptions(view, 'solution_design', 3).at(-1)).toEqual({ version: 3, label: '3, writing' });
  });
});

describe('shownStatus', () => {
  const view = viewAtDesignGate();
  const [need, , designV1, designV2] = view.artifacts;

  it.each([
    ['the open gate', designV2, false, 'At your gate', 'waiting'],
    ['a superseded version', designV1, false, 'Superseded', 'old'],
    ['a finished stage', need, false, 'Done', 'ok'],
    ['a version being written', null, true, 'Writing', 'running'],
  ])('%s', (_name, stored, writing, text, tone) => {
    expect(shownStatus(view, stored?.stage ?? 'solution_design', stored, writing)).toEqual({ text, tone });
  });

  it('waits on the open question on the current stage', () => {
    const asking = { ...view, open: { prompt_id: 'Q4', kind: 'question' as const } };
    expect(shownStatus(asking, 'solution_design', designV2, false)).toEqual({ text: 'Waiting on Q4', tone: 'waiting' });
  });

  it('says approved for a decided gate', () => {
    expect(shownStatus(view, 'solution_design', ref('solution_design', 2, 'approved'), false).text).toBe('Approved');
  });
});

describe('reviewFor', () => {
  const view = viewAtDesignGate();

  it('shows a revision the review of the version it revised', () => {
    expect(reviewFor(view, 'solution_design', 2)).toMatchObject({ version: 1, review: { score: 4 } });
    expect(reviewFor(view, 'solution_design', 1)?.version).toBe(1);
    expect(reviewFor(view, 'intake', 1)).toBeNull();
  });
});
