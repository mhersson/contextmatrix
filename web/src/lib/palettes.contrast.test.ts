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

// OKLab distance: about 0.02 is a just-noticeable difference.
function oklab(hex: string): [number, number, number] {
  const [r, g, b] = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

function distance(a: string, b: string): number {
  const [x, y] = [oklab(a), oklab(b)];
  return Math.hypot(x[0] - y[0], x[1] - y[1], x[2] - y[2]);
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
      for (const fg of ['fg', 'grey0', 'grey1', 'grey2', 'aqua', 'orange']) {
        for (const bg of ['bg-dim', 'bg0', 'bg1', 'bg2']) {
          expect(contrast(vars[fg], vars[bg]), `${mode}: --${fg} on --${bg}`).toBeGreaterThanOrEqual(4.5);
        }
      }
    });

    // --aqua marks a card an agent is working on and focus rings; --orange
    // marks high priority and major findings. grey1 and grey2 are muted and
    // tertiary text. Each pair has to read as two colours.
    it(`${mode}: roles the board tells apart look different`, () => {
      expect(distance(vars.aqua, vars.orange), `${mode}: --aqua vs --orange`).toBeGreaterThanOrEqual(0.08);
      expect(distance(vars.grey1, vars.grey2), `${mode}: --grey1 vs --grey2`).toBeGreaterThanOrEqual(0.03);
    });
  }
});
