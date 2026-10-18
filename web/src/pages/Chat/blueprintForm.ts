export type BlueprintMode = 'default' | 'auto' | 'auto+emit' | 'yolo';

export interface BlueprintForm {
  projects: string[];
  mode: BlueprintMode;
  /** USD; 0 means no blueprint cap. */
  capUSD: number;
}

export const EMPTY_BLUEPRINT_FORM: BlueprintForm = { projects: [], mode: 'default', capUSD: 0 };

export const BLUEPRINT_MODES: { value: BlueprintMode; label: string; description: string }[] = [
  {
    value: 'default',
    label: 'Default',
    description:
      'You answer the questions and decide the gates that need you. Cards go on the board when you approve the sign-off; you start them.',
  },
  {
    value: 'auto',
    label: 'Auto',
    description:
      'An AI responder answers questions and gates for you. Stops at the sign-off; nothing reaches the board.',
  },
  {
    value: 'auto+emit',
    label: 'Auto + emit',
    description: 'The responder answers everything and the cards go on the board. Nothing runs until you start them.',
  },
  {
    value: 'yolo',
    label: 'Yolo',
    description: 'The responder answers everything; the cards go on the board and start running.',
  },
];

/** Cap presets in USD; 0 is No cap. */
export const CAP_PRESETS: readonly number[] = [1, 2, 5, 10, 0];

export function capLabel(cap: number): string {
  return cap > 0 ? `$${cap}` : 'No cap';
}

export function capSentence(cap: number): string {
  return cap > 0
    ? `The architect stops before it spends more than $${cap} on this blueprint.`
    : "No blueprint cap; each stage run keeps the architect's own limit.";
}

export const UNPRICED_CAP_REASON =
  "No price is known for this model, so a cost cap can't be enforced: pick No cap or another model";

/**
 * Why Create is disabled, or null when it is enabled. `modelUnpriced` is true
 * when the picked blueprint model is in the list and CM cannot price it.
 */
export function disabledReason(blueprint: boolean, form: BlueprintForm, modelUnpriced = false): string | null {
  if (!blueprint) return null;
  if (form.projects.length === 0) return 'Pick at least one project to create the blueprint.';
  if (form.capUSD > 0 && modelUnpriced) {
    return UNPRICED_CAP_REASON;
  }
  return null;
}

export function createLabel(blueprint: boolean, mode: BlueprintMode, creating: boolean): string {
  if (creating) return blueprint ? 'Starting the architect' : 'Starting';
  if (!blueprint) return 'Create chat';
  return mode === 'yolo' ? 'Create and run' : 'Create blueprint';
}
