import { describe, it, expect } from 'vitest';
import type { BlueprintView } from '../../types';
import { formatHHMM } from '../../utils/chatTimestamp';
import { LEDGER_REVIEW_V1, LEDGER_REVIEW_V2, THREE_SLICES, ref, step, viewAtDesignGate, viewEndedAtSlicing, viewEndedInSlice, viewOneSlice, viewSliced } from '../../test-utils/blueprintFixtures';
import {
  artifactsFor,
  barGroups,
  sliceSuffix,
  costLabel,
  currentStep,
  decidedByText,
  describeCurrent,
  finished,
  latestArtifact,
  normalizeView,
  personaName,
  stageText,
  stepFor,
  stepStatusText,
} from './stages';

function at(patch: Partial<BlueprintView>): BlueprintView {
  return { ...viewAtDesignGate(), ...patch };
}

describe('stage names', () => {
  it('maps known stages to their sheet tabs and humanizes unknown ones', () => {
    expect(stageText('solution_design').tab).toBe('Design');
    expect(stageText('decomposition').tab).toBe('Cards');
    expect(stageText('emission').tab).toBe('Board');
    expect(stageText('threat_model').tab).toBe('Threat model');
    expect(personaName('senior_developer')).toBe('Senior developer');
  });
});

describe('normalizeView', () => {
  it('treats missing or null arrays as empty', () => {
    const raw = { ...viewAtDesignGate(), pipeline: null, artifacts: undefined } as unknown as BlueprintView;
    const view = normalizeView(raw);
    expect(view.pipeline).toEqual([]);
    expect(view.artifacts).toEqual([]);
    const withNullFindings = normalizeView({
      ...viewAtDesignGate(),
      artifacts: [ref('solution_design', 1, 'superseded', { review: { reviewer: 'r', score: 3, threshold: 4, findings: null as never } })],
    });
    expect(withNullFindings.artifacts[0].review?.findings).toEqual([]);
    expect(describeCurrent(view)).toBeNull();
  });
});

describe('currentStep', () => {
  it('prefers the open gate stage over the running run', () => {
    const view = at({ current: { stage: 'grounding', kind: 'stage', persona: 'senior_developer', model: 'm', started_at: '', turns: 1, tool_calls: 0, tokens: 0 } });
    expect(currentStep(view)?.stage).toBe('solution_design');
  });
});

describe('describeCurrent', () => {
  const run = (kind: 'stage' | 'reviewer' | 'responder', persona: string, stage = 'solution_design') => ({
    stage, kind, persona, model: 'm', started_at: '2026-10-17T10:00:00Z', turns: 6, tool_calls: 4, tokens: 11200,
  });

  it.each([
    ['at your gate', at({}), 'Design rev 2 is at your gate', 'waiting'],
    ['a question waits', at({ open: { prompt_id: 'Q4', kind: 'question' }, current: run('stage', 'system_designer') }), 'Q4 is waiting for you', 'waiting'],
    ['a round waits', at({ open: { prompt_id: 'round:1', kind: 'round' }, current: undefined }), 'Questions are waiting for you', 'waiting'],
    ['the responder answers', at({ open: undefined, current: run('responder', 'system_designer') }), 'The responder is answering a question', 'running'],
    ['the responder answers an open question', at({ open: { prompt_id: 'Q4', kind: 'question' }, current: run('responder', 'system_designer') }), 'The responder is answering Q4', 'running'],
    ['a reviewer reviews', at({ open: undefined, current: run('reviewer', 'senior_developer') }), 'Senior developer is reviewing design rev 2', 'running'],
  ])('%s', (_name, view, line, tone) => {
    const text = describeCurrent(view)!;
    expect(text.name).toBe('Solution design');
    expect(text.line).toBe(line);
    expect(text.tone).toBe(tone);
  });

  it('counts the findings a revision answers', () => {
    const view = at({
      open: undefined,
      current: run('stage', 'system_designer'),
      pipeline: viewAtDesignGate().pipeline.map((s) => (s.stage === 'solution_design' ? { ...s, status: 'revising' as const, latest_version: 1 } : s)),
      artifacts: viewAtDesignGate().artifacts.filter((a) => !(a.stage === 'solution_design' && a.version === 2)),
    });
    expect(describeCurrent(view)!.line).toBe('System designer is revising the design (3 findings)');
  });

  it('counts only the findings the reviewer has not marked fixed', () => {
    // Rev 2 passed with two findings open, so the builder revises it for rev 3.
    const view = at({
      open: undefined,
      current: run('stage', 'system_designer'),
      pipeline: viewAtDesignGate().pipeline.map((s) =>
        s.stage === 'solution_design' ? { ...s, status: 'revising' as const, latest_version: 2 } : s,
      ),
      artifacts: [
        ref('intake', 1, 'current'),
        ref('slicing', 1, 'folded'),
        ref('solution_design', 1, 'superseded', { review: LEDGER_REVIEW_V1 }),
        ref('solution_design', 2, 'current', { review: LEDGER_REVIEW_V2 }),
      ],
    });
    expect(describeCurrent(view)!.line).toBe('System designer is revising the design (2 findings)');
  });

  it('names the readiness round', () => {
    const pipeline = viewAtDesignGate().pipeline.map((s) =>
      s.stage === 'readiness' ? { ...s, status: 'running' as const, round: 2, max_rounds: 3 } : s.status === 'pending' || s.status === 'waiting' ? { ...s, status: 'done' as const } : s,
    );
    const view = at({ open: undefined, pipeline, current: run('stage', 'reviewer', 'readiness') });
    expect(describeCurrent(view)!.line).toBe('Reviewer is checking the cards, round 2 of 3');
  });

  it('says the cards are on the board once everything is done', () => {
    const pipeline = viewAtDesignGate().pipeline.map((s) => (s.status === 'folded' ? s : { ...s, status: 'done' as const }));
    expect(describeCurrent(at({ open: undefined, pipeline, state: 'emitted' }))).toMatchObject({ name: 'Emission', tone: 'done', line: 'Cards on the board; watching their runs' });
    expect(describeCurrent(at({ open: undefined, pipeline, state: 'done' }))!.line).toBe('Cards on the board');
  });

  it('marks a failed stage', () => {
    const pipeline = viewAtDesignGate().pipeline.map((s) => (s.stage === 'solution_design' ? { ...s, status: 'failed' as const } : s));
    expect(describeCurrent(at({ open: undefined, pipeline }))!.tone).toBe('failed');
  });

  it('waits on the operator for an emission resume without naming an artifact', () => {
    const pipeline = viewAtDesignGate().pipeline.map((s) =>
      s.stage === 'emission' ? { ...s, status: 'waiting' as const } : s.status === 'folded' ? s : { ...s, status: 'done' as const },
    );
    const artifacts = viewAtDesignGate().artifacts.map((a) => (a.status === 'at_gate' ? { ...a, status: 'approved' as const } : a));
    const text = describeCurrent(at({ pipeline, artifacts, open: { prompt_id: 'emission:v1', kind: 'gate' } }))!;
    expect(text).toEqual({ name: 'Emission', line: 'Waiting for you', tone: 'waiting' });
  });
});

describe('stepStatusText', () => {
  it('says who approved a gate and when', () => {
    const view = at({
      artifacts: [ref('solution_design', 2, 'approved', { decided_by: 'you', decided_at: '2026-10-17T10:17:00Z' })],
    });
    const s = step('solution_design', 'system_designer', 'done', { gate: true });
    expect(stepStatusText(s, view)).toBe(`Approved by you, ${formatHHMM('2026-10-17T10:17:00Z')}`);
    expect(decidedByText(ref('x', 1, 'approved', { decided_by: 'automatic' }))).toBe('automatically');
    // Architects before protocol v0.28.0 named the responder for an auto stage gate the reviewer's verdict decides.
    expect(decidedByText(ref('x', 1, 'approved', { decided_by: 'responder' }))).toBe('automatically');
  });

  it('names what a folded stage folded into', () => {
    expect(stepStatusText(viewAtDesignGate().pipeline[1], viewAtDesignGate())).toBe('Folded into design');
  });
});

describe('costLabel', () => {
  const cost = { spent_usd: 0.35, run_spent_usd: 0, cap_usd: 2, run_cap_usd: 0, priced: true };
  it.each([
    ['under the cap', cost, '$0.35 of $2.00', 'ok'],
    ['from 80 percent', { ...cost, spent_usd: 1.6 }, '$1.60 of $2.00', 'warn'],
    ['at the cap', { ...cost, spent_usd: 2 }, '$2.00 of $2.00', 'over'],
    ['no cap', { ...cost, cap_usd: 0 }, '$0.35, no cap', 'ok'],
    ['unpriced, no cap', { ...cost, cap_usd: 0, priced: false }, '$0.35, some runs unpriced', 'ok'],
    ['unpriced with a cap', { ...cost, priced: false }, '$0.35 of $2.00, some runs unpriced', 'ok'],
  ])('%s', (_name, c, text, tone) => {
    expect(costLabel(c)).toMatchObject({ text, tone });
  });

  it('draws a meter only against a cap', () => {
    expect(costLabel({ ...cost, cap_usd: 0 }).percent).toBeNull();
    expect(costLabel(cost).percent).toBeCloseTo(17.5);
  });
});

describe('slices', () => {
  it('fills a missing slice list', () => {
    const raw = { ...viewAtDesignGate(), slices: undefined } as unknown as BlueprintView;
    expect(normalizeView(raw).slices).toEqual([]);
  });

  it("keeps each slice's versions of a stage apart", () => {
    const view = viewSliced();
    expect(artifactsFor(view, 'solution_design', 'SL1').map((a) => a.status)).toEqual(['approved']);
    expect(latestArtifact(view, 'solution_design', 'SL2')?.status).toBe('at_gate');
    expect(artifactsFor(view, 'solution_design', '')).toEqual([]);
  });

  it('finds the current step in the slice the gate or run belongs to', () => {
    const view = viewSliced();
    expect(currentStep(view)).toMatchObject({ stage: 'solution_design', slice: 'SL2' });
    view.open = undefined;
    view.artifacts = view.artifacts.filter((a) => a.status !== 'at_gate');
    view.current = { stage: 'solution_design', slice: 'SL3', kind: 'stage', persona: 'system_designer', model: 'm', started_at: '', turns: 1, tool_calls: 0, tokens: 0 };
    expect(currentStep(view)).toMatchObject({ stage: 'solution_design', slice: 'SL3' });
  });

  it("reads each slice step's status from its own versions", () => {
    const view = viewSliced();
    expect(stepStatusText(stepFor(view, 'solution_design', 'SL1')!, view)).toBe('Approved by you');
    expect(stepStatusText(stepFor(view, 'solution_design', 'SL2')!, view)).toBe('Waiting for you');
  });
});

describe('slice names', () => {
  it('suffixes a slice only while the blueprint shows slices', () => {
    expect(sliceSuffix(THREE_SLICES, 'SL2')).toBe(' · SL2');
    expect(sliceSuffix(THREE_SLICES, '')).toBe('');
    expect(sliceSuffix([THREE_SLICES[0]], 'SL1')).toBe('');
  });

  it('names the slice in the current stage', () => {
    expect(describeCurrent(viewSliced())?.name).toBe('Solution design · SL2');
    expect(describeCurrent(viewOneSlice())?.name).toBe('Solution design');
  });

  it("groups each slice's steps and leaves the rest alone", () => {
    const groups = barGroups(viewSliced());
    expect(groups.map((g) => [g.slice, g.steps.length])).toEqual([
      ['', 1], ['', 1], ['', 1], ['SL1', 4], ['SL2', 4], ['SL3', 4], ['', 1], ['', 1], ['', 1],
    ]);
    expect(barGroups(viewOneSlice()).every((g) => g.slice === '' && g.steps.length === 1)).toBe(true);
  });
});

describe('an ended blueprint', () => {
  it("carries a plain-language blueprint's technical reason apart from the plain one", () => {
    const view = {
      ...viewEndedAtSlicing(),
      plain_language: true,
      end_reason: 'The architect found nothing it could build on.',
      end_detail: 'slicing stored no slices, so nothing could be designed',
    };
    const text = describeCurrent(view)!;
    expect(text.line).toContain(view.end_reason);
    expect(text.line).not.toContain(view.end_detail);
    expect(text.detail).toBe(view.end_detail);
    expect(describeCurrent(viewEndedAtSlicing())!.detail).toBeUndefined();
    expect(describeCurrent({ ...view, end_detail: ' ' })!.detail).toBeUndefined();
  });

  it('never reads finished and stands on the step that stopped it', () => {
    const view = viewEndedAtSlicing();
    expect(finished(view)).toBe(false);
    expect(currentStep(view)?.stage).toBe('slicing');
    const allDone = { ...view, pipeline: view.pipeline.map((s) => ({ ...s, status: 'done' as const })) };
    expect(finished(allDone)).toBe(false);
  });

  it('says why the pipeline stopped, in the failed tone', () => {
    const view = viewEndedAtSlicing();
    const text = describeCurrent(view)!;
    expect(text).toMatchObject({ name: stageText('slicing').name, tone: 'failed' });
    expect(text.line).toContain(view.end_reason!);
    const bare = describeCurrent({ ...view, end_reason: undefined })!;
    expect(bare.tone).toBe('failed');
    expect(bare.line).not.toBe('');
    expect(bare.line).not.toMatch(/:\s*$/);
  });

  it('names the slice whose step stopped the pipeline', () => {
    expect(describeCurrent(viewEndedInSlice())!.name).toBe(`${stageText('decomposition').name} · SL2`);
  });

  it('tells a stopped step and one never reached apart from a pending one', () => {
    const view = viewEndedAtSlicing();
    const texts = [step('grounding', 'senior_developer', 'pending'), view.pipeline[1], view.pipeline[2]].map((s) => stepStatusText(s, view));
    expect(new Set(texts).size).toBe(3);
  });
});
