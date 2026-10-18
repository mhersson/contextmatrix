import type { BlueprintArtifactRef, BlueprintArtifactReview, BlueprintView, LogEntry } from '../../types';
import { derivePromptStates, parsePromptEntry, type ChatPrompt } from '../ChatPanel/prompts';
import type { RoundLeft } from '../QuestionRound/rounds';
import type { HeldDraft } from './draft';
import {
  artifactsFor,
  currentStep,
  decidedByText,
  finished,
  latestArtifact,
  sliceOf,
  stageText,
  stepFor,
  visibleSteps,
} from './stages';

export interface StampText {
  tone: 'approved' | 'rejected' | 'superseded' | 'folded';
  title: string;
  detail: string;
}

/** The decision stamp a version carries, if any; who decided a gate shows here, not in the title block. */
export function stampFor(view: BlueprintView, ref: BlueprintArtifactRef): StampText | null {
  switch (ref.status) {
    case 'approved':
      return { tone: 'approved', title: 'Approved', detail: decidedByText(ref) };
    case 'rejected':
      return { tone: 'rejected', title: 'Rejected', detail: decidedByText(ref) };
    case 'superseded': {
      const next = artifactsFor(view, ref.stage, sliceOf(ref)).find((r) => r.version > ref.version);
      return { tone: 'superseded', title: 'Superseded', detail: next ? `by rev ${next.version}` : '' };
    }
    case 'folded': {
      const into = stepFor(view, ref.stage, sliceOf(ref))?.folds_into;
      return { tone: 'folded', title: 'Folded', detail: into ? `into ${stageText(into).noun}` : '' };
    }
    default:
      return null;
  }
}

export interface VersionOption {
  version: number;
  label: string;
}

export function versionOptions(
  view: BlueprintView,
  stage: string,
  slice: string,
  writingVersion: number | null,
): VersionOption[] {
  const refs = artifactsFor(view, stage, slice);
  const options = refs.map((r) => ({
    version: r.version,
    label:
      r.status === 'superseded' || r.status === 'rejected' || r.status === 'folded'
        ? `${r.version}, ${r.status}`
        : String(r.version),
  }));
  if (writingVersion !== null && !refs.some((r) => r.version === writingVersion)) {
    options.push({ version: writingVersion, label: `${writingVersion}, writing` });
  }
  return options;
}

export type StatusTone = 'running' | 'waiting' | 'ok' | 'old' | 'bad' | 'muted';

export interface ShownStatus {
  text: string;
  tone: StatusTone;
}

/** What the stage waits on: the open question, or the open round and what it still lacks (null when not loaded). */
export type Waiting = { kind: 'question'; id: string } | { kind: 'round'; left: RoundLeft | null };

export function waitingOn(view: BlueprintView, stage: string, slice: string, roundLeft: RoundLeft | null): Waiting | null {
  const cur = currentStep(view);
  if (cur?.stage !== stage || sliceOf(cur) !== slice) return null;
  if (view.open?.kind === 'question') return { kind: 'question', id: view.open.prompt_id };
  if (view.open?.kind === 'round') return { kind: 'round', left: roundLeft };
  return null;
}

const count = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;

/** "Q4", "3 questions", "2 assumptions", or "your answers" once only the submit is missing. */
export function waitingLabel(w: Waiting): string {
  if (w.kind === 'question') return w.id;
  if (w.left && w.left.questions > 0) return count(w.left.questions, 'question');
  if (w.left && w.left.assumptions > 0) return count(w.left.assumptions, 'assumption');
  return 'your answers';
}

/** The sheet's waiting line. */
export function waitingLine(w: Waiting): string {
  return w.kind === 'question' ? `${w.id} is waiting for you` : `Waiting on ${waitingLabel(w)}`;
}

export function shownStatus(
  view: BlueprintView,
  stage: string,
  slice: string,
  stored: BlueprintArtifactRef | null,
  writing: boolean,
  roundLeft: RoundLeft | null = null,
): ShownStatus {
  // The asking stage stays current while its question or round waits, so the wait outranks Writing.
  const waiting = waitingOn(view, stage, slice, roundLeft);
  if (waiting && (writing || (stored !== null && stored.version === latestArtifact(view, stage, slice)?.version))) {
    return { text: `Waiting on ${waitingLabel(waiting)}`, tone: 'waiting' };
  }
  if (writing) return { text: 'Writing', tone: 'running' };
  if (!stored) return { text: '', tone: 'muted' };
  // A failed step leaves its newest version current (or reviewing on older architects); that must not read as done.
  if (
    stepFor(view, stage, slice)?.status === 'failed' &&
    stored.version === latestArtifact(view, stage, slice)?.version &&
    (stored.status === 'current' || stored.status === 'reviewing')
  ) {
    return { text: stored.review ? 'Failed' : 'Review failed', tone: 'bad' };
  }
  switch (stored.status) {
    case 'reviewing':
      return { text: 'Being reviewed', tone: 'running' };
    case 'at_gate':
      return { text: view.open?.kind === 'gate' ? 'At your gate' : 'At the gate', tone: 'waiting' };
    case 'approved':
      return { text: 'Approved', tone: 'ok' };
    case 'rejected':
      return { text: 'Rejected', tone: 'bad' };
    case 'superseded':
      return { text: 'Superseded', tone: 'old' };
    case 'folded':
      return { text: 'Folded', tone: 'muted' };
    default:
      return { text: 'Done', tone: 'ok' };
  }
}

export interface ShownReview {
  review: BlueprintArtifactReview;
  /** The version the reviewer scored. */
  version: number;
}

/** The review behind a version: its own, else the one of the version it revised. */
export function reviewFor(view: BlueprintView, stage: string, slice: string, version: number): ShownReview | null {
  const reviewed = artifactsFor(view, stage, slice)
    .filter((r) => r.version <= version && r.review)
    .pop();
  return reviewed?.review ? { review: reviewed.review, version: reviewed.version } : null;
}

/** The reviewer at work on this sheet; it always reviews the newest stored version. */
export function reviewingPersona(view: BlueprintView, stage: string, slice: string, shownVersion: number): string | null {
  const cur = view.current;
  if (cur?.kind !== 'reviewer' || cur.stage !== stage || sliceOf(cur) !== slice) return null;
  return latestArtifact(view, stage, slice)?.version === shownVersion ? cur.persona : null;
}

export interface Shown {
  stage: string;
  slice: string;
  version: number;
}

/** The version a run is writing for a stage; null when nothing is being written there. */
export function writingVersion(
  view: BlueprintView,
  draft: HeldDraft | null,
  stage: string,
  slice: string,
): number | null {
  const stored = artifactsFor(view, stage, slice);
  const next = (stored[stored.length - 1]?.version ?? 0) + 1;
  if (draft && draft.stage === stage && draft.slice === slice && !stored.some((r) => r.version === draft.version)) {
    return draft.version;
  }
  // A round ends its stage run, yet the stage still owes its next version while the round waits.
  const cur = currentStep(view);
  if (view.open?.kind === 'round' && cur?.stage === stage && sliceOf(cur) === slice) return next;
  const run = view.current;
  // A responder run answers the asking stage's question mid-run, so that stage is still writing.
  if (run && run.kind !== 'reviewer' && run.stage === stage && sliceOf(run) === slice) return next;
  return null;
}

export interface SheetTab {
  stage: string;
  slice: string;
  label: string;
  enabled: boolean;
  current: boolean;
}

export function sheetTabs(view: BlueprintView, draft: HeldDraft | null): SheetTab[] {
  const current = finished(view) ? null : currentStep(view);
  return visibleSteps(view).map((s) => ({
    stage: s.stage,
    slice: sliceOf(s),
    label: stageText(s.stage).tab,
    enabled:
      artifactsFor(view, s.stage, sliceOf(s)).length > 0 || writingVersion(view, draft, s.stage, sliceOf(s)) !== null,
    current: s === current,
  }));
}

/** The current stage's version being written or its newest, else the last stage with an artifact. */
export function defaultShown(view: BlueprintView, draft: HeldDraft | null): Shown | null {
  const step = currentStep(view);
  if (step) {
    const slice = sliceOf(step);
    const writing = writingVersion(view, draft, step.stage, slice);
    if (writing !== null) return { stage: step.stage, slice, version: writing };
    const latest = latestArtifact(view, step.stage, slice);
    if (latest) return { stage: step.stage, slice, version: latest.version };
  }
  for (const s of [...view.pipeline].reverse()) {
    const latest = latestArtifact(view, s.stage, sliceOf(s));
    if (latest) return { stage: s.stage, slice: sliceOf(s), version: latest.version };
  }
  return null;
}

export function resolveShown(
  view: BlueprintView,
  draft: HeldDraft | null,
  pin: { stage: string; slice: string; version: number | null } | null,
): Shown | null {
  if (pin) {
    const versions = artifactsFor(view, pin.stage, pin.slice).map((r) => r.version);
    const writing = writingVersion(view, draft, pin.stage, pin.slice);
    if (pin.version !== null && (versions.includes(pin.version) || pin.version === writing)) {
      return { stage: pin.stage, slice: pin.slice, version: pin.version };
    }
    if (writing !== null) return { stage: pin.stage, slice: pin.slice, version: writing };
    if (versions.length > 0) return { stage: pin.stage, slice: pin.slice, version: versions[versions.length - 1] };
  }
  return defaultShown(view, draft);
}

export interface GatePrompt {
  prompt: ChatPrompt;
  /** Paused while the session is not live: the bar stays, its buttons disabled as in the transcript. */
  status: 'open' | 'paused';
}

/** The unanswered transcript prompt that is the gate the view reports open, matched on the prompt id. */
export function gatePromptFor(
  logs: readonly LogEntry[],
  live: boolean,
  openGateId: string | undefined,
): GatePrompt | null {
  if (!openGateId) return null;
  for (const [entry, state] of derivePromptStates(logs, live)) {
    if (state.status !== 'open' && state.status !== 'paused') continue;
    const prompt = parsePromptEntry(entry)?.prompt;
    if (prompt?.id === openGateId) return { prompt, status: state.status };
  }
  return null;
}
