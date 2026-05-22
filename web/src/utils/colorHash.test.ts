import { describe, expect, it } from 'vitest';
import { avatarGradient } from './colorHash';

describe('avatarGradient', () => {
  it('returns two distinct CSS variable references', () => {
    const g = avatarGradient('claude-haiku-4.5');
    expect(g.from).toMatch(/^var\(--/);
    expect(g.to).toMatch(/^var\(--/);
    expect(g.from).not.toBe(g.to);
  });
});
