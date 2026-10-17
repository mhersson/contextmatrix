import type {
  BlueprintArtifactRef,
  BlueprintCost,
  BlueprintPipelineStep,
  BlueprintStepStatus,
  BlueprintView,
} from '../../types';
import { formatHHMM } from '../../utils/chatTimestamp';

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
}

const STAGES: Record<string, StageText> = {
  intake: { tab: 'Need', name: 'Intake', title: 'Need', noun: 'need', doing: 'working on the need' },
  slicing: { tab: 'Slices', name: 'Slicing', title: 'Slices', noun: 'slices', doing: 'working on the slices' },
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

export function normalizeView(view: BlueprintView): BlueprintView {
  return {
    ...view,
    pipeline: view.pipeline ?? [],
    artifacts: (view.artifacts ?? []).map((a) =>
      a.review ? { ...a, review: { ...a.review, findings: a.review.findings ?? [] } } : a,
    ),
  };
}

export function artifactsFor(view: BlueprintView, stage: string): BlueprintArtifactRef[] {
  return view.artifacts.filter((a) => a.stage === stage).sort((a, b) => a.version - b.version);
}

export function latestArtifact(view: BlueprintView, stage: string): BlueprintArtifactRef | undefined {
  const list = artifactsFor(view, stage);
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

/** Every step the blueprint runs is done or folded. */
export function finished(view: BlueprintView): boolean {
  const steps = visibleSteps(view);
  return steps.length > 0 && steps.every((s) => s.status === 'done' || s.status === 'folded');
}

const ACTIVE = new Set<BlueprintStepStatus>(['running', 'reviewing', 'revising', 'waiting', 'failed']);

export function currentStep(view: BlueprintView): BlueprintPipelineStep | null {
  const steps = visibleSteps(view);
  const named = gateArtifact(view)?.stage ?? view.current?.stage;
  const byName = named ? steps.find((s) => s.stage === named) : undefined;
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
}

export function describeCurrent(view: BlueprintView): CurrentText | null {
  const step = currentStep(view);
  if (!step) return null;
  const st = stageText(step.stage);
  const say = (line: string, tone: Tone): CurrentText => ({ name: st.name, line, tone });
  const run = view.current;
  const open = view.open;
  const persona = personaName(run?.persona ?? step.persona);

  if (finished(view)) {
    if (step.stage !== 'emission') return say('Done', 'done');
    return say(view.state === 'done' ? 'Cards on the board' : 'Cards on the board; watching their runs', 'done');
  }
  // The responder only answers questions; under an auto policy the question has no open prompt.
  if (run?.kind === 'responder') {
    return say(
      open?.kind === 'question' ? `The responder is answering ${open.prompt_id}` : 'The responder is answering a question',
      'running',
    );
  }
  if (open?.kind === 'gate') {
    // An emission resume is a gate with no artifact at it.
    const at = gateArtifact(view);
    if (!at) return say('Waiting for you', 'waiting');
    return say(`${stageText(at.stage).tab} rev ${at.version} is at your gate`, 'waiting');
  }
  if (open?.kind === 'question') return say(`${open.prompt_id} is waiting for you`, 'waiting');
  if (run?.kind === 'reviewer') {
    return say(`${persona} is reviewing ${st.noun} rev ${step.latest_version || 1}`, 'running');
  }
  if (step.status === 'failed') return say('This stage failed; the transcript says why', 'failed');
  if (step.status === 'revising') {
    const n = latestArtifact(view, step.stage)?.review?.findings.length ?? 0;
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
    case 'responder':
      return 'by the responder';
    case 'automatic':
      return 'automatically';
    default:
      return '';
  }
}

export function stepStatusText(step: BlueprintPipelineStep, view: BlueprintView): string {
  switch (step.status) {
    case 'done': {
      const latest = latestArtifact(view, step.stage);
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
