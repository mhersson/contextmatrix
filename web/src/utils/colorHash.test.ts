import { describe, expect, it } from 'vitest';
import { idColor, avatarGradient } from './colorHash';

describe('idColor', () => {
  it('returns a stable color for the same id', () => {
    expect(idColor('claude-haiku-4.5')).toBe(idColor('claude-haiku-4.5'));
  });
});

describe('avatarGradient', () => {
  it('returns two distinct CSS variable references', () => {
    const g = avatarGradient('claude-haiku-4.5');
    expect(g.from).toMatch(/^var\(--/);
    expect(g.to).toMatch(/^var\(--/);
    expect(g.from).not.toBe(g.to);
  });

  it('is stable for the same id', () => {
    expect(avatarGradient('opus-4.7')).toEqual(avatarGradient('opus-4.7'));
  });
});
