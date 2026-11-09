import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// Reads the Copper blocks from index.css so the check follows the shipped
// values. Body and secondary text must clear WCAG AA (4.5:1) on every
// surface it sits on, in both modes.
const css = readFileSync(resolve(process.cwd(), 'src/index.css'), 'utf8');

function block(selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  expect(start, `${selector} block`).toBeGreaterThanOrEqual(0);
  const body = css.slice(start, css.indexOf('}', start));
  const vars: Record<string, string> = {};
  for (const m of body.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})/gi)) vars[m[1]] = m[2];
  return vars;
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

describe('copper palette contrast', () => {
  const modes = {
    dark: block('[data-palette="copper"]'),
    light: block('[data-palette="copper"][data-theme="light"]'),
  };

  for (const [mode, vars] of Object.entries(modes)) {
    it(`${mode}: text and the copper accent clear AA on the canvas and raised surfaces`, () => {
      for (const fg of ['fg', 'grey0', 'grey1', 'grey2', 'aqua']) {
        for (const bg of ['bg-dim', 'bg0', 'bg1', 'bg2']) {
          expect(contrast(vars[fg], vars[bg]), `${mode}: --${fg} on --${bg}`).toBeGreaterThanOrEqual(4.5);
        }
      }
    });
  }
});
