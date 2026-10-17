import type { BlueprintArtifactRef, BlueprintArtifactReview, BlueprintView } from '../../types';
import { artifactsFor, currentStep, decidedByText, latestArtifact, stageText } from './stages';

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
      const newest = latestArtifact(view, ref.stage);
      return { tone: 'superseded', title: 'Superseded', detail: newest ? `by rev ${newest.version}` : '' };
    }
    case 'folded': {
      const into = view.pipeline.find((s) => s.stage === ref.stage)?.folds_into;
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

export function versionOptions(view: BlueprintView, stage: string, writingVersion: number | null): VersionOption[] {
  const refs = artifactsFor(view, stage);
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

export function shownStatus(
  view: BlueprintView,
  stage: string,
  stored: BlueprintArtifactRef | null,
  writing: boolean,
): ShownStatus {
  if (writing) return { text: 'Writing', tone: 'running' };
  if (!stored) return { text: '', tone: 'muted' };
  const newest = latestArtifact(view, stage);
  if (stored.version === newest?.version && view.open?.kind === 'question' && currentStep(view)?.stage === stage) {
    return { text: `Waiting on ${view.open.prompt_id}`, tone: 'waiting' };
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
export function reviewFor(view: BlueprintView, stage: string, version: number): ShownReview | null {
  const reviewed = artifactsFor(view, stage)
    .filter((r) => r.version <= version && r.review)
    .pop();
  return reviewed?.review ? { review: reviewed.review, version: reviewed.version } : null;
}
