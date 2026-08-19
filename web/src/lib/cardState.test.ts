import { describe, expect, it } from 'vitest';
import { isTerminalState } from './cardState';

describe('isTerminalState', () => {
  it('is true for done', () => {
    expect(isTerminalState('done')).toBe(true);
  });

  it('is true for not_planned', () => {
    expect(isTerminalState('not_planned')).toBe(true);
  });

  it('is false for stalled', () => {
    expect(isTerminalState('stalled')).toBe(false);
  });

  it('is false for todo and in_progress', () => {
    expect(isTerminalState('todo')).toBe(false);
    expect(isTerminalState('in_progress')).toBe(false);
  });

  it('is false for an unknown custom state', () => {
    expect(isTerminalState('custom_state')).toBe(false);
  });
});
