import { describe, expect, it } from 'vitest';
import { displayState } from './stateLabels';

describe('displayState', () => {
  it('falls back to a title-cased word-split for unknown states', () => {
    expect(displayState('custom_state')).toBe('Custom State');
  });
});
