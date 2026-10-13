import { expect } from 'vitest';
import { within } from '@testing-library/react';
import { safeUrlTransform } from '../utils/safeUrlTransform';

// Markup an external card author (agent, GitHub or Jira import) can plant in a
// body. Every markdown surface must show it as text: no live elements, no
// injected attributes, and no content hidden from the human reading it.
export const RAW_HTML_BODY = [
  '<iframe srcdoc="<script>parent.alert(document.domain)</script>"></iframe>',
  '',
  '<script>window.markdownXss = true</script>',
  '',
  '<img src="x" onerror="window.markdownXss = true">',
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
  expect(outerHTMLOf(root, 'iframe, script, object, embed')).toEqual([]);
  expect(outerHTMLOf(root, '[style]')).toEqual([]);

  const withHandlers = Array.from(root.querySelectorAll('*'))
    .filter((el) => Array.from(el.attributes).some((attr) => attr.name.toLowerCase().startsWith('on')))
    .map((el) => el.outerHTML);
  expect(withHandlers).toEqual([]);

  const unsafeUrls = Array.from(root.querySelectorAll('[href], [src]'))
    .map((el) => el.getAttribute('href') ?? el.getAttribute('src') ?? '')
    .filter((url) => safeUrlTransform(url) !== url);
  expect(unsafeUrls).toEqual([]);

  expect(root.textContent).toContain('<iframe srcdoc=');
  expect(root.textContent).toContain('<script>');
  expect(root.textContent).toContain('onerror=');
  expect(root.textContent).toContain('text the agent sees');
}

export function expectMarkdownRendered(root: HTMLElement) {
  const heading = within(root).getByRole('heading', { level: 1, name: /Overview/ });
  expect(heading.querySelector('a[href="#overview"]')).not.toBeNull();
  expect(within(root).getByRole('link', { name: 'the docs' })).toHaveAttribute(
    'href',
    'https://example.com/docs',
  );
  expect(root.querySelector('code')?.textContent).toBe('inlineCode');
  expect(root.querySelector('pre code')?.textContent).toContain('const answer = 42;');
}
