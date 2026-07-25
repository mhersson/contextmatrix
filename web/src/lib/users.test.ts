import { describe, expect, it } from 'vitest';
import { userInitials, userLabel } from './users';

describe('userInitials', () => {
  it('uses first and last word of a two-word display name', () => {
    expect(userInitials('Morten Hersson', 'mohersson')).toBe('MH');
  });

  it('uses first and LAST word of a longer display name', () => {
    expect(userInitials('Anna Louise van Berg', 'avb')).toBe('AB');
  });

  it('falls back to the username initial for a single-word display name', () => {
    expect(userInitials('Plato', 'zeus')).toBe('Z');
  });

  it('falls back to the username initial for empty, whitespace, or missing display names', () => {
    expect(userInitials('', 'bob')).toBe('B');
    expect(userInitials('   ', 'bob')).toBe('B');
    expect(userInitials(undefined, 'bob')).toBe('B');
  });
});

describe('userLabel', () => {
  it('prefers the display name', () => {
    expect(userLabel({ display_name: 'Alice Smith', username: 'alice' })).toBe('Alice Smith');
  });

  it('falls back to the username when the display name is empty', () => {
    expect(userLabel({ display_name: '', username: 'bob' })).toBe('bob');
  });
});
