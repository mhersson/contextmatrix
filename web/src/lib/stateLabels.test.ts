import { describe, expect, it } from 'vitest';
import { STATE_DISPLAY_LABEL, displayState } from './stateLabels';

describe('STATE_DISPLAY_LABEL', () => {
  it('maps known states to display labels', () => {
    expect(STATE_DISPLAY_LABEL.todo).toBe('Backlog');
    expect(STATE_DISPLAY_LABEL.in_progress).toBe('In Progress');
    expect(STATE_DISPLAY_LABEL.review).toBe('In Review');
    expect(STATE_DISPLAY_LABEL.done).toBe('Shipped');
    expect(STATE_DISPLAY_LABEL.stalled).toBe('Stalled');
    expect(STATE_DISPLAY_LABEL.not_planned).toBe('Not Planned');
  });
});

describe('displayState', () => {
  it('returns the mapped label for known states', () => {
    expect(displayState('done')).toBe('Shipped');
    expect(displayState('in_progress')).toBe('In Progress');
  });

  it('falls back to a title-cased word-split for unknown states', () => {
    expect(displayState('custom_state')).toBe('Custom State');
  });
});
