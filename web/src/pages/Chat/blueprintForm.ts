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

/** Why Create is disabled, or null when it is enabled. */
export function disabledReason(blueprint: boolean, form: BlueprintForm): string | null {
  return blueprint && form.projects.length === 0 ? 'Pick at least one project to create the blueprint.' : null;
}

export function createLabel(blueprint: boolean, mode: BlueprintMode, creating: boolean): string {
  if (creating) return blueprint ? 'Starting the architect' : 'Starting';
  if (!blueprint) return 'Create chat';
  return mode === 'yolo' ? 'Create and run' : 'Create blueprint';
}
