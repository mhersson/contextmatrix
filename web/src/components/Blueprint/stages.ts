import type {
  BlueprintArtifactRef,
  BlueprintCost,
  BlueprintPipelineStep,
  BlueprintSlice,
  BlueprintStepStatus,
  BlueprintView,
} from '../../types';
import { formatHHMM } from '../../utils/chatTimestamp';
import { unresolvedCount } from './ledger';

export interface StageText {
  /** Sheet tab and sheet-reference label. */
  tab: string;
  /** Scale bar and stage divider name. */
  name: string;
  /** Sheet heading. */
  title: string;
  /** The artifact inside a sentence. */
  noun: string;
  /** What the persona does while the stage runs. */
  doing: string;
  /** A fixed subtitle under the sheet title, where the stage has one. */
  sub?: string;
}

const STAGES: Record<string, StageText> = {
  intake: { tab: 'Need', name: 'Intake', title: 'Need', noun: 'need', doing: 'working on the need' },
  slicing: { tab: 'Slices', name: 'Slicing', title: 'Slices', noun: 'slices', doing: 'working on the slices' },
  shared_decisions: {
    tab: 'Shared',
    name: 'Shared decisions',
    title: 'Shared decisions',
    noun: 'shared decisions',
    doing: 'settling the shared decisions',
    sub: 'Decisions every slice builds on',
  },
  solution_design: {
    tab: 'Design',
    name: 'Solution design',
    title: 'Solution design',
    noun: 'design',
    doing: 'working on the design',
  },
  grounding: {
    tab: 'Grounding',
    name: 'Grounding',
    title: 'Grounding',
    noun: 'grounding',
    doing: 'checking the design against the code',
  },
  acceptance: {
    tab: 'Acceptance',
    name: 'Acceptance',
    title: 'Acceptance',
    noun: 'acceptance criteria',
    doing: 'working on the acceptance criteria',
  },
  decomposition: { tab: 'Cards', name: 'Decomposition', title: 'Cards', noun: 'cards', doing: 'working on the cards' },
  readiness: { tab: 'Readiness', name: 'Readiness', title: 'Readiness', noun: 'readiness', doing: 'checking the cards' },
  signoff: { tab: 'Sign-off', name: 'Sign-off', title: 'Sign-off', noun: 'sign-off', doing: 'preparing the sign-off' },
  mockup: { tab: 'Mockup', name: 'Mockup', title: 'Mockup', noun: 'mockup', doing: 'drawing the mockup' },
  emission: {
    tab: 'Board',
    name: 'Emission',
    title: 'On the board',
    noun: 'board',
    doing: 'putting the cards on the board',
  },
};

/** "solution_design" -> "Solution design". */
export function humanize(id: string): string {
  const words = id.replace(/[_-]+/g, ' ').trim();
  return words === '' ? '' : words[0].toUpperCase() + words.slice(1);
}

export function stageText(stage: string): StageText {
  const known = STAGES[stage];
  if (known) return known;
  const name = humanize(stage);
  return { tab: name, name, title: name, noun: name.toLowerCase(), doing: `working on ${name.toLowerCase()}` };
}

export function personaName(persona: string | undefined): string {
  return persona ? humanize(persona) : 'The architect';
}

/** The piece a step, artifact, run or draft belongs to; "" for the whole blueprint. */
export function sliceOf(x: { slice?: string } | null | undefined): string {
  return x?.slice ?? '';
}

/** The pipeline step behind a stage's sheet in one slice. */
export function stepFor(view: BlueprintView, stage: string, slice: string): BlueprintPipelineStep | undefined {
  return view.pipeline.find((s) => s.stage === stage && sliceOf(s) === slice);
}

/** Slice tabs, brackets and slice names show only for a blueprint cut into two or more slices. */
export function showSlices(slices: readonly BlueprintSlice[]): boolean {
  return slices.length >= 2;
}

/** A stage whose steps repeat once per slice. */
export function isPerSlice(view: BlueprintView, stage: string): boolean {
  return view.pipeline.some((s) => s.stage === stage && sliceOf(s) !== '');
}

export function normalizeView(view: BlueprintView): BlueprintView {
  return {
    ...view,
    slices: view.slices ?? [],
    pipeline: view.pipeline ?? [],
    artifacts: (view.artifacts ?? []).map((a) =>
      a.review ? { ...a, review: { ...a.review, findings: a.review.findings ?? [] } } : a,
    ),
  };
}

export function artifactsFor(view: BlueprintView, stage: string, slice: string): BlueprintArtifactRef[] {
  return view.artifacts.filter((a) => a.stage === stage && sliceOf(a) === slice).sort((a, b) => a.version - b.version);
}

export function latestArtifact(view: BlueprintView, stage: string, slice: string): BlueprintArtifactRef | undefined {
  const list = artifactsFor(view, stage, slice);
  return list[list.length - 1];
}

/**
 * The version waiting at the open gate; null when no gate is open or the open
 * gate is not an artifact's (an emission resume). Prompt ids are matched by
 * equality elsewhere and never parsed.
 */
export function gateArtifact(view: BlueprintView): BlueprintArtifactRef | null {
  if (view.open?.kind !== 'gate') return null;
  const waiting = view.artifacts.filter((a) => a.status === 'at_gate').sort((a, b) => a.version - b.version);
  return waiting[waiting.length - 1] ?? null;
}

/** The steps the pack runs; skipped steps show nowhere. */
export function visibleSteps(view: BlueprintView): BlueprintPipelineStep[] {
  return view.pipeline.filter((s) => s.status !== 'skipped');
}

/** Every step the blueprint runs is done or folded; an ended blueprint never finished. */
export function finished(view: BlueprintView): boolean {
  if (view.state === 'ended') return false;
  const steps = visibleSteps(view);
  return steps.length > 0 && steps.every((s) => s.status === 'done' || s.status === 'folded');
}

const ACTIVE = new Set<BlueprintStepStatus>(['running', 'reviewing', 'revising', 'waiting', 'failed']);

export function currentStep(view: BlueprintView): BlueprintPipelineStep | null {
  const steps = visibleSteps(view);
  // Nothing runs on an ended blueprint, so it stands where the step that ended it stopped.
  const stopped = view.state === 'ended' ? steps.find((s) => s.status === 'stopped') : undefined;
  if (stopped) return stopped;
  const named = gateArtifact(view) ?? view.current;
  const byName = named ? steps.find((s) => s.stage === named.stage && sliceOf(s) === sliceOf(named)) : undefined;
  return (
    byName ??
    steps.find((s) => ACTIVE.has(s.status)) ??
    steps.find((s) => s.status === 'pending') ??
    steps[steps.length - 1] ??
    null
  );
}

export type Tone = 'running' | 'waiting' | 'failed' | 'idle' | 'done';

export interface CurrentText {
  /** The stage name, shown in the display face. */
  name: string;
  /** Who is doing what. */
  line: string;
  tone: Tone;
  /** The technical reason behind a plain end reason; folded under the line. */
  detail?: string;
}

/** " · SL2" after a stage name while the blueprint shows slices; "" otherwise. */
export function sliceSuffix(slices: readonly BlueprintSlice[], slice: string): string {
  return showSlices(slices) && slice !== '' ? ` · ${slice}` : '';
}

export interface BarGroup {
  /** "" for a whole-blueprint step, which stands alone. */
  slice: string;
  steps: BlueprintPipelineStep[];
}

/** The bar's steps in runs: one slice's consecutive steps bracket together while the blueprint shows slices. */
export function barGroups(view: BlueprintView): BarGroup[] {
  const grouped = showSlices(view.slices);
  const groups: BarGroup[] = [];
  for (const s of visibleSteps(view)) {
    const slice = grouped ? sliceOf(s) : '';
    const last = groups[groups.length - 1];
    if (slice !== '' && last?.slice === slice) last.steps.push(s);
    else groups.push({ slice, steps: [s] });
  }
  return groups;
}

export function describeCurrent(view: BlueprintView): CurrentText | null {
  const step = currentStep(view);
  if (!step) return null;
  const st = stageText(step.stage);
  const name = st.name + sliceSuffix(view.slices, sliceOf(step));
  const say = (line: string, tone: Tone): CurrentText => ({ name, line, tone });
  const run = view.current;
  const open = view.open;
  const persona = personaName(run?.persona ?? step.persona);

  if (finished(view)) {
    if (step.stage !== 'emission') return say('Done', 'done');
    return say(view.state === 'done' ? 'Cards on the board' : 'Cards on the board; watching their runs', 'done');
  }
  if (view.state === 'ended') {
    const text = say(view.end_reason ? `Stopped: ${view.end_reason}` : 'Stopped', 'failed');
    const detail = view.end_detail?.trim();
    return detail ? { ...text, detail } : text;
  }
  // The responder answers questions (under an auto policy with no open prompt) and tries the mockup.
  if (run?.kind === 'responder' && run.stage === 'mockup') return say('The responder is trying the mockup', 'running');
  if (run?.kind === 'responder') {
    return say(
      open?.kind === 'question' ? `The responder is answering ${open.prompt_id}` : 'The responder is answering a question',
      'running',
    );
  }
  if (open?.kind === 'mockup') {
    const rev = latestArtifact(view, 'mockup', '')?.version;
    return say(rev ? `Mockup rev ${rev} is waiting for you` : 'The mockup is waiting for you', 'waiting');
  }
  // Between the responder's turns the mockup step waits with no prompt open; nobody waits on the person.
  if (step.stage === 'mockup' && step.status === 'waiting' && !open) {
    return say('The mockup is with the responder', 'running');
  }
  if (open?.kind === 'gate') {
    // An emission resume is a gate with no artifact at it.
    const at = gateArtifact(view);
    if (!at) return say('Waiting for you', 'waiting');
    return say(`${stageText(at.stage).tab} rev ${at.version} is at your gate`, 'waiting');
  }
  if (open?.kind === 'question') return say(`${open.prompt_id} is waiting for you`, 'waiting');
  if (open?.kind === 'round') return say('Questions are waiting for you', 'waiting');
  if (run?.kind === 'reviewer') {
    return say(`${persona} is reviewing ${st.noun} rev ${step.latest_version || 1}`, 'running');
  }
  if (step.status === 'failed') return say('This stage failed; the transcript says why', 'failed');
  if (step.status === 'revising') {
    const review = latestArtifact(view, step.stage, sliceOf(step))?.review;
    const n = review ? unresolvedCount(review) : 0;
    const why = n > 0 ? ` (${n} ${n === 1 ? 'finding' : 'findings'})` : '';
    return say(`${persona} is revising the ${st.noun}${why}`, 'running');
  }
  if (run || step.status === 'running') {
    const round = step.round && step.max_rounds ? `, round ${step.round} of ${step.max_rounds}` : '';
    return say(`${persona} is ${st.doing}${round}`, 'running');
  }
  if (step.status === 'reviewing') return say('Being reviewed', 'running');
  if (step.status === 'waiting') return say('Waiting for you', 'waiting');
  return say(`Up next: ${personaName(step.persona)}`, 'idle');
}

export function decidedByText(ref: BlueprintArtifactRef): string {
  switch (ref.decided_by) {
    case 'you': {
      const time = ref.decided_at ? formatHHMM(ref.decided_at) : null;
      return time ? `by you, ${time}` : 'by you';
    }
    // Older architects sent "responder" for an auto stage gate; the reviewer's verdict decides those.
    case 'responder':
    case 'automatic':
      return 'automatically';
    default:
      return '';
  }
}

export function stepStatusText(step: BlueprintPipelineStep, view: BlueprintView): string {
  switch (step.status) {
    case 'done': {
      const latest = latestArtifact(view, step.stage, sliceOf(step));
      return step.gate && latest?.status === 'approved' ? `Approved ${decidedByText(latest)}`.trim() : 'Done';
    }
    case 'folded':
      return step.folds_into ? `Folded into ${stageText(step.folds_into).noun}` : 'Folded';
    case 'running':
      return 'Running';
    case 'reviewing':
      return 'Being reviewed';
    case 'revising':
      return 'Revising';
    case 'waiting':
      return 'Waiting for you';
    case 'failed':
      return 'Failed';
    case 'skipped':
      return 'Skipped';
    case 'stopped':
      return 'Stopped the pipeline';
    case 'not_reached':
      return 'Not reached';
    default:
      return 'Not started';
  }
}

export function money(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

export interface CostText {
  text: string;
  /** Meter fill against the cap; null without a cap. */
  percent: number | null;
  tone: 'ok' | 'warn' | 'over';
}

export function costLabel(cost: BlueprintCost): CostText {
  const spent = money(cost.spent_usd);
  const unpriced = cost.priced ? '' : ', some runs unpriced';
  if (cost.cap_usd <= 0) {
    return { text: cost.priced ? `${spent}, no cap` : `${spent}${unpriced}`, percent: null, tone: 'ok' };
  }
  const percent = Math.min(100, (cost.spent_usd / cost.cap_usd) * 100);
  const tone = cost.spent_usd >= cost.cap_usd ? 'over' : percent >= 80 ? 'warn' : 'ok';
  return { text: `${spent} of ${money(cost.cap_usd)}${unpriced}`, percent, tone };
}
