import { describe, it, expect } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useSheetState, type SheetLayout } from './useSheetState';
import { holdDraft, type HeldDraft } from './draft';
import { ref, viewAtDesignGate, viewSliced } from '../../test-utils/blueprintFixtures';
import type { BlueprintView } from '../../types';

interface Props {
  view: BlueprintView | null;
  draft: HeldDraft | null;
  layout: SheetLayout;
}

function setup(initial: Props) {
  return renderHook((p: Props) => useSheetState(p.view, p.draft, p.layout), { initialProps: initial });
}

function idle(): BlueprintView {
  return { ...viewAtDesignGate(), open: undefined };
}

function withNewVersion(view: BlueprintView): BlueprintView {
  return { ...view, artifacts: [...view.artifacts, ref('solution_design', 3, 'current')] };
}

describe('useSheetState', () => {
  it('starts open beside the transcript and closed over it', () => {
    expect(setup({ view: idle(), draft: null, layout: 'split' }).result.current.open).toBe(true);
    expect(setup({ view: idle(), draft: null, layout: 'overlay' }).result.current.open).toBe(false);
  });

  it('does not open for the first view it sees', () => {
    const { result, rerender } = setup({ view: null, draft: null, layout: 'overlay' });
    rerender({ view: idle(), draft: null, layout: 'overlay' });
    expect(result.current.open).toBe(false);
  });

  it('opens a closed sheet for a new artifact version', () => {
    const view = idle();
    const { result, rerender } = setup({ view, draft: null, layout: 'overlay' });
    rerender({ view: withNewVersion(view), draft: null, layout: 'overlay' });
    expect(result.current.open).toBe(true);
  });

  it('opens over the transcript at a gate', () => {
    const view = viewAtDesignGate();
    const { result, rerender } = setup({ view, draft: null, layout: 'overlay' });
    rerender({ view: withNewVersion(view), draft: null, layout: 'overlay' });
    expect(result.current.open).toBe(true);
  });

  it('only marks something new over the transcript while a question waits', () => {
    const asking = { ...idle(), open: { prompt_id: 'Q4', kind: 'question' as const } };
    const { result, rerender } = setup({ view: asking, draft: null, layout: 'phone' });
    rerender({ view: withNewVersion(asking), draft: null, layout: 'phone' });
    expect(result.current.open).toBe(false);
    expect(result.current.hasNew).toBe(true);
    act(() => result.current.openSheet());
    expect(result.current).toMatchObject({ open: true, hasNew: false });
  });

  it('opens a split sheet even while a question waits', () => {
    const asking = { ...idle(), open: { prompt_id: 'Q4', kind: 'question' as const } };
    const { result, rerender } = setup({ view: asking, draft: null, layout: 'split' });
    act(() => result.current.close());
    rerender({ view: withNewVersion(asking), draft: null, layout: 'split' });
    expect(result.current.open).toBe(true);
  });

  it('opens when a draft starts, not when it ends', () => {
    const view = idle();
    const draft = holdDraft({ stage: 'solution_design', version: 3, offset: 0, text: 'x' });
    const { result, rerender } = setup({ view, draft: null, layout: 'overlay' });
    rerender({ view, draft, layout: 'overlay' });
    expect(result.current.open).toBe(true);
    act(() => result.current.close());
    rerender({ view, draft: null, layout: 'overlay' });
    expect(result.current.open).toBe(false);
  });

  it('holds a pin across snapshots until the current stage changes', () => {
    const { result, rerender } = setup({ view: viewAtDesignGate(), draft: null, layout: 'split' });
    act(() => result.current.show('intake', '', 1));
    rerender({ view: viewAtDesignGate(), draft: null, layout: 'split' });
    expect(result.current.pin).toEqual({ stage: 'intake', slice: '', version: 1 });
    const grounding = {
      ...idle(),
      current: { stage: 'grounding', kind: 'stage' as const, persona: 'senior_developer', model: 'm', started_at: '', turns: 1, tool_calls: 0, tokens: 0 },
    };
    rerender({ view: grounding, draft: null, layout: 'split' });
    expect(result.current.pin).toBeNull();
  });
  it('only marks something new over the transcript while a round waits', () => {
    const asking = { ...idle(), open: { prompt_id: 'round:1', kind: 'round' as const } };
    const { result, rerender } = setup({ view: asking, draft: null, layout: 'phone' });
    rerender({ view: withNewVersion(asking), draft: null, layout: 'phone' });
    expect(result.current.open).toBe(false);
    expect(result.current.hasNew).toBe(true);
  });

  it('opens a closed sheet for a new version in another slice of the same stage', () => {
    const view = { ...viewSliced(), open: undefined };
    const { result, rerender } = setup({ view, draft: null, layout: 'overlay' });
    rerender({ view: { ...view, artifacts: [...view.artifacts, ref('solution_design', 1, 'current', { slice: 'SL3' })] }, draft: null, layout: 'overlay' });
    expect(result.current.open).toBe(true);
  });

  it('drops a pin when the current step moves to another slice of the same stage', () => {
    const { result, rerender } = setup({ view: viewSliced(), draft: null, layout: 'split' });
    act(() => result.current.show('grounding', 'SL1', null));
    expect(result.current.pin).toEqual({ stage: 'grounding', slice: 'SL1', version: null });
    const next = viewSliced();
    next.open = undefined;
    next.artifacts = next.artifacts.map((a) => (a.status === 'at_gate' ? { ...a, status: 'approved' as const } : a));
    next.current = { stage: 'solution_design', slice: 'SL3', kind: 'stage', persona: 'system_designer', model: 'm', started_at: '', turns: 0, tool_calls: 0, tokens: 0 };
    rerender({ view: next, draft: null, layout: 'split' });
    expect(result.current.pin).toBeNull();
  });

  it('remembers the last slice picked across a whole-blueprint sheet', () => {
    const { result } = setup({ view: viewSliced(), draft: null, layout: 'split' });
    act(() => result.current.show('solution_design', 'SL1', null));
    act(() => result.current.show('intake', '', null));
    expect(result.current.picked).toBe('SL1');
  });

  it('forgets the picked slice when the current step moves', () => {
    const { result, rerender } = setup({ view: viewSliced(), draft: null, layout: 'split' });
    act(() => result.current.show('solution_design', 'SL1', null));
    expect(result.current.picked).toBe('SL1');
    const next = viewSliced();
    next.open = undefined;
    next.artifacts = next.artifacts.map((a) => (a.status === 'at_gate' ? { ...a, status: 'approved' as const } : a));
    next.current = { stage: 'solution_design', slice: 'SL3', kind: 'stage', persona: 'system_designer', model: 'm', started_at: '', turns: 0, tool_calls: 0, tokens: 0 };
    rerender({ view: next, draft: null, layout: 'split' });
    expect(result.current.picked).toBe('');
  });
});
