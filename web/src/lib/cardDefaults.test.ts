import { describe, it, expect } from 'vitest';
import {
  BUILTIN_CARD_DEFAULTS,
  cardDefaultsKey,
  resolveCardDefaults,
  toWireCardDefaults,
} from './cardDefaults';

describe('resolveCardDefaults', () => {
  it('returns the built-ins for an absent block', () => {
    expect(resolveCardDefaults(undefined)).toEqual(BUILTIN_CARD_DEFAULTS);
    expect(resolveCardDefaults(null)).toEqual(BUILTIN_CARD_DEFAULTS);
    expect(BUILTIN_CARD_DEFAULTS.create_pr).toBe(true);
    expect(BUILTIN_CARD_DEFAULTS.autonomous).toBe(false);
    expect(BUILTIN_CARD_DEFAULTS.mob_participants).toBe(0);
  });

  it('keeps create_pr on when the block omits it', () => {
    expect(resolveCardDefaults({ autonomous: true }).create_pr).toBe(true);
  });

  it('honours an explicit create_pr false', () => {
    expect(resolveCardDefaults({ create_pr: false }).create_pr).toBe(false);
  });

  it('fills every field concretely', () => {
    expect(resolveCardDefaults({ mob_participants: 3, mob_phases: ['review'], await_ci: true })).toEqual({
      autonomous: false,
      max_capability: false,
      mob_participants: 3,
      mob_phases: ['review'],
      create_pr: true,
      await_ci: true,
      await_copilot_review: false,
    });
  });
});

describe('cardDefaultsKey', () => {
  it('is stable across phase order', () => {
    const a = resolveCardDefaults({ mob_participants: 3, mob_phases: ['plan', 'review'] });
    const b = resolveCardDefaults({ mob_participants: 3, mob_phases: ['review', 'plan'] });
    expect(cardDefaultsKey(a)).toBe(cardDefaultsKey(b));
  });

  it('differs when a flag differs', () => {
    expect(cardDefaultsKey(resolveCardDefaults({ autonomous: true }))).not.toBe(
      cardDefaultsKey(BUILTIN_CARD_DEFAULTS),
    );
  });
});

describe('toWireCardDefaults', () => {
  it('sends every field explicitly and drops phases when seats are off', () => {
    expect(toWireCardDefaults({ ...BUILTIN_CARD_DEFAULTS, mob_phases: ['review'] })).toEqual({
      autonomous: false,
      max_capability: false,
      mob_participants: 0,
      create_pr: true,
      await_ci: false,
      await_copilot_review: false,
    });
  });

  it('keeps phases when seats are on', () => {
    expect(toWireCardDefaults({ ...BUILTIN_CARD_DEFAULTS, mob_participants: 3, mob_phases: ['review'] }).mob_phases)
      .toEqual(['review']);
  });
});
