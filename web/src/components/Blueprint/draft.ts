import type { ArtifactDraft } from '../../types';
import { sliceOf } from './stages';

/** The draft the pane holds; length counts code points, as the architect's offsets do. */
export interface HeldDraft {
  stage: string;
  slice: string;
  version: number;
  text: string;
  length: number;
  refused: boolean;
  reason: string;
}

export function codePoints(text: string): number {
  return Array.from(text).length;
}

export function holdDraft(d: ArtifactDraft): HeldDraft {
  return {
    stage: d.stage,
    slice: sliceOf(d),
    version: d.version,
    text: d.text,
    length: codePoints(d.text),
    refused: d.refused === true,
    reason: d.reason ?? '',
  };
}

/**
 * Offset 0 replaces the held text; an offset at the held length appends; any
 * other frame means frames were missed, so it is dropped and the next
 * snapshot frame, which carries the whole draft, heals the gap.
 */
export function applyDraft(held: HeldDraft | null, d: ArtifactDraft): HeldDraft | null {
  if (d.offset === 0) return holdDraft(d);
  if (!held || held.stage !== d.stage || held.slice !== sliceOf(d) || held.version !== d.version || held.length !== d.offset) {
    return held;
  }
  return {
    ...held,
    text: held.text + d.text,
    length: held.length + codePoints(d.text),
    refused: d.refused === true,
    reason: d.reason ?? '',
  };
}
