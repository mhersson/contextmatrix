import type { BlueprintArtifactRef, BlueprintView } from '../../types';
import { decidedByText, latestArtifact, stageText } from './stages';

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
