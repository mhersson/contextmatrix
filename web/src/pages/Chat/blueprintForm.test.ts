import { describe, it, expect } from 'vitest';
import { EMPTY_BLUEPRINT_FORM, capSentence, createLabel, disabledReason } from './blueprintForm';

describe('blueprintForm', () => {
  it('blocks a blueprint without projects and nothing else', () => {
    expect(disabledReason(true, EMPTY_BLUEPRINT_FORM)).not.toBeNull();
    expect(disabledReason(true, { ...EMPTY_BLUEPRINT_FORM, projects: ['alpha'] })).toBeNull();
    expect(disabledReason(false, EMPTY_BLUEPRINT_FORM)).toBeNull();
  });

  it('names the cap amount only when there is a cap', () => {
    expect(capSentence(5)).toContain('$5');
    expect(capSentence(0)).not.toContain('$');
  });

  it('labels yolo differently from the other modes and the creating state', () => {
    expect(createLabel(true, 'yolo', false)).not.toBe(createLabel(true, 'default', false));
    expect(createLabel(true, 'auto', false)).toBe(createLabel(true, 'default', false));
    expect(createLabel(true, 'default', true)).not.toBe(createLabel(false, 'default', true));
  });
});
