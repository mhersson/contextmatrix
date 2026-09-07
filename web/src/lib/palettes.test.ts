import { describe, it, expect } from 'vitest';
import { PALETTES, isPalette } from './palettes';

describe('isPalette', () => {
  it('accepts every registered palette id', () => {
    for (const p of PALETTES) {
      expect(isPalette(p.id)).toBe(true);
    }
  });

  it('accepts the new github and ayu palettes', () => {
    expect(isPalette('github')).toBe(true);
    expect(isPalette('ayu')).toBe(true);
  });

  it('rejects the removed radix palette and arbitrary strings', () => {
    expect(isPalette('radix')).toBe(false);
    expect(isPalette('')).toBe(false);
    expect(isPalette('monokai')).toBe(false);
  });
});
