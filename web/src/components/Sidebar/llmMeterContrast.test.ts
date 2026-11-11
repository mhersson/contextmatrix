import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, it, expect } from 'vitest';

const css = readFileSync(resolve(__dirname, '../../index.css'), 'utf8');

function ruleBody(selector: string): string {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const m = new RegExp(`(?:^|\\n)${esc}\\s*\\{([^}]*)\\}`).exec(css);
  if (!m) throw new Error(`rule not found: ${selector}`);
  return m[1];
}

function hexTokens(selector: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of ruleBody(selector).matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) {
    out[m[1]] = m[2];
  }
  return out;
}

// Later layers override earlier ones: :root, the palette's dark block, the theme.
const palettes: Record<string, string[]> = {
  'everforest dark': [':root'],
  'everforest light': [':root', '[data-theme="light"]'],
};
for (const p of ['catppuccin', 'github', 'ayu']) {
  palettes[`${p} dark`] = [':root', `[data-palette="${p}"]`];
  palettes[`${p} light`] = [
    ':root',
    `[data-palette="${p}"]`,
    `[data-palette="${p}"][data-theme="light"]`,
  ];
}

function tokensFor(name: string): Record<string, string> {
  return Object.assign({}, ...palettes[name].map(hexTokens));
}

function luminance(hex: string): number {
  const ch = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function tokenOf(selector: string, prop: string, fallback?: string): string {
  const m = new RegExp(`${prop}:[^;]*?var\\((--[\\w-]+)\\)`).exec(ruleBody(selector));
  if (m) return m[1];
  if (fallback) return fallback;
  throw new Error(`no ${prop} token in ${selector}`);
}

describe('LLM channel usage meter contrast', () => {
  const track = tokenOf('.llm-meter__bar', 'background-color');
  const outline = tokenOf('.llm-meter__bar', 'box-shadow', track);
  const fills = {
    normal: tokenOf('.llm-meter__fill', 'background-color'),
    warn: tokenOf('.llm-meter__fill--warn', 'background-color'),
    stop: tokenOf('.llm-meter__fill--stop', 'background-color'),
  };

  describe.each(Object.keys(palettes))('%s', (name) => {
    const t = tokensFor(name);

    it.each(Object.entries(fills))('%s fill stands clear of the track', (_state, token) => {
      expect(contrast(t[token], t[track])).toBeGreaterThanOrEqual(1.8);
    });

    it('track outline stands clear of the popover surface', () => {
      expect(contrast(t[outline], t['--bg2'])).toBeGreaterThanOrEqual(1.4);
    });
  });
});
