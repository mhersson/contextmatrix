import { BLUEPRINT_MODES, type BlueprintMode } from '../../pages/Chat/blueprintForm';
import type { BlueprintView } from '../../types';

// The architect refuses a switch from emission on; done follows emitted.
const EMISSION_STARTED: ReadonlySet<string> = new Set(['emitting', 'emitted', 'done']);

// An approved sign-off counts as emission started, even while a blocked preflight leaves the state idle.
function signedOff(view: BlueprintView): boolean {
  return view.pipeline.some((s) => s.stage === 'signoff' && s.status === 'done');
}

export function isBlueprintMode(mode: string): mode is BlueprintMode {
  return BLUEPRINT_MODES.some((m) => m.value === mode);
}

/** The new-blueprint dialog's label for a mode; a mode it does not know shows as the view sends it. */
export function modeLabel(mode: string): string {
  return BLUEPRINT_MODES.find((m) => m.value === mode)?.label ?? mode;
}

/** Why the mode no longer changes, or null while it can. */
export function modeLock(view: BlueprintView): string | null {
  if (view.state === 'ended') return 'The pipeline has ended, so the mode no longer changes.';
  if (EMISSION_STARTED.has(view.state) || signedOff(view)) return 'The sign-off is approved, so the mode no longer changes.';
  return null;
}

/** What a switch to `next` does at once to what waits on you; null when it changes nothing that waits. */
export function switchNote(view: BlueprintView, next: BlueprintMode): string | null {
  // Only a switch out of Default hands a human-held round or gate over; the other modes already hold what waits for a reason.
  if (next === 'default') return null;
  const fromDefault = view.mode === 'default';
  if (fromDefault && view.open?.kind === 'round') {
    return 'The questions waiting for you go to the responder now; answers you have not sent are dropped.';
  }
  if (fromDefault && view.open?.kind === 'mockup') {
    return 'The mockup waiting for you goes to the responder now; it starts from the settings you last sent.';
  }
  const atSignoff =
    view.open?.kind === 'gate' && view.pipeline.some((s) => s.stage === 'signoff' && s.status === 'waiting');
  if (atSignoff && (next === 'auto+emit' || next === 'yolo') && view.mode !== 'auto+emit' && view.mode !== 'yolo') {
    return 'The sign-off waiting for you is decided automatically now; a critical finding or a preflight warning still leaves it to you.';
  }
  if (fromDefault && view.open?.kind === 'gate' && !atSignoff) {
    return 'The gate waiting for you is approved now if its review passed, unless a gate reply keeps that stage with you.';
  }
  return null;
}
