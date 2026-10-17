import { describe, it, expect } from 'vitest';
import { formatHHMM } from '../../utils/chatTimestamp';
import { ref, viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import { holdDraft } from './draft';
import { defaultShown, resolveShown, reviewFor, sheetTabs, shownStatus, stampFor, versionOptions, writingVersion } from './sheetModel';

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

const running = (stage: string) => ({
  stage,
  kind: 'stage' as const,
  persona: 'system_designer',
  model: 'm',
  started_at: '',
  turns: 1,
  tool_calls: 0,
  tokens: 0,
});

describe('which sheet shows', () => {
  it('follows the current stage and its newest version', () => {
    expect(defaultShown(viewAtDesignGate(), null)).toEqual({ stage: 'solution_design', version: 2 });
  });

  it('shows the version a run is writing', () => {
    const view = { ...viewAtDesignGate(), open: undefined, current: running('solution_design') };
    expect(writingVersion(view, null, 'solution_design')).toBe(3);
    const draft = holdDraft({ stage: 'solution_design', version: 3, offset: 0, text: 'x' });
    expect(writingVersion(viewAtDesignGate(), draft, 'solution_design')).toBe(3);
    expect(defaultShown(view, null)).toEqual({ stage: 'solution_design', version: 3 });
  });

  it('falls back to the last stage with an artifact', () => {
    const view = { ...viewAtDesignGate(), open: undefined };
    view.pipeline = view.pipeline.map((s) => (s.stage === 'solution_design' ? { ...s, status: 'done' as const } : s));
    expect(defaultShown(view, null)).toEqual({ stage: 'solution_design', version: 2 });
  });

  it('keeps a pinned version and drops a pin that no longer exists', () => {
    const view = viewAtDesignGate();
    expect(resolveShown(view, null, { stage: 'solution_design', version: 1 })).toEqual({ stage: 'solution_design', version: 1 });
    expect(resolveShown(view, null, { stage: 'intake', version: null })).toEqual({ stage: 'intake', version: 1 });
    expect(resolveShown(view, null, { stage: 'solution_design', version: 9 })).toEqual({ stage: 'solution_design', version: 2 });
  });
});

describe('sheetTabs', () => {
  it('hides skipped stages, dims stages without artifacts and marks the current one', () => {
    const view = viewAtDesignGate();
    view.pipeline = view.pipeline.map((s) => (s.stage === 'emission' ? { ...s, status: 'skipped' as const } : s));
    const tabs = sheetTabs(view, null);
    expect(tabs.map((t) => t.label)).toEqual(['Need', 'Slices', 'Design', 'Grounding', 'Acceptance', 'Cards', 'Readiness', 'Sign-off']);
    expect(tabs.filter((t) => t.enabled).map((t) => t.stage)).toEqual(['intake', 'slicing', 'solution_design']);
    expect(tabs.filter((t) => t.current).map((t) => t.stage)).toEqual(['solution_design']);
  });
});
