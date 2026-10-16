export type BlueprintMode = 'default' | 'auto' | 'auto+emit' | 'yolo';

export interface BlueprintDraft {
  projects: string[];
  mode: BlueprintMode;
  /** Raw input text; parsed to a number at submit time. */
  costCapUSD: string;
}

export const EMPTY_BLUEPRINT_DRAFT: BlueprintDraft = { projects: [], mode: 'default', costCapUSD: '' };

export const BLUEPRINT_MODES: { value: BlueprintMode; label: string }[] = [
  { value: 'default', label: 'Default: human gates as the pack sets them' },
  { value: 'auto', label: 'Auto: AI answers every gate, stops before emission' },
  { value: 'auto+emit', label: 'Auto + emit: also emits the cards' },
  { value: 'yolo', label: 'Yolo: emits and runs the cards' },
];
